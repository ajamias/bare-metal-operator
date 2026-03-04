/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/ajamias/bare-metal-operator/api/v1alpha1"
)

// BareMetalClusterReconciler reconciles a BareMetalCluster object
type BareMetalClusterReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	HttpClient        *http.Client
	OsacInventoryUrl  *url.URL
	OsacManagementUrl *url.URL
	AuthToken         string
}

const BareMetalClusterFinalizer = "osac.openshift.io/cluster-request"

// HostResponse represents the response from the inventory service
type HostResponse struct {
	Hosts []Host `json:"nodes"`
}

// TODO: replace this with future Host type
// Host represents a single host from the inventory
type Host struct {
	NodeId         string         `json:"uuid"`
	HostClass      string         `json:"resource_class"`
	ProvisionState string         `json:"provision_state"` // available, active, etc
	Extra          map[string]any `json:"extra"`           // contains matchType and clusterId
}

type HostAction int

const (
	HostAttach HostAction = iota
	HostDetach
)

// HostAttachmentOperation represents a single host attach/detach operation
type HostAttachmentAction struct {
	Host      *Host
	ClusterId string
	Action    HostAction
}

// +kubebuilder:rbac:groups=osac.openshift.io,resources=baremetalclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=osac.openshift.io,resources=baremetalclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=osac.openshift.io,resources=baremetalclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=osac.openshift.io,resources=testhosts,verbs=get;list;watch;create;update;patch;delete;deletecollection

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *BareMetalClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	bareMetalCluster := &v1alpha1.BareMetalCluster{}
	err := r.Get(ctx, req.NamespacedName, bareMetalCluster)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Info(
		"Starting reconcile",
		"namespacedName",
		req.NamespacedName,
		"name",
		bareMetalCluster.Name,
	)

	oldstatus := bareMetalCluster.Status.DeepCopy()

	var result ctrl.Result
	if !bareMetalCluster.DeletionTimestamp.IsZero() {
		result, err = r.handleDeletion(ctx, bareMetalCluster)
	} else {
		result, err = r.handleUpdate(ctx, bareMetalCluster)
	}

	if !equality.Semantic.DeepEqual(bareMetalCluster.Status, *oldstatus) {
		status := metav1.ConditionFalse
		hostsReason := meta.FindStatusCondition(
			bareMetalCluster.Status.Conditions,
			v1alpha1.BareMetalClusterConditionTypeHostsReady,
		).Reason

		var reason string
		switch hostsReason {
		case v1alpha1.BareMetalClusterReasonHostsAvailable:
			status = metav1.ConditionTrue
			reason = v1alpha1.BareMetalClusterReasonReady
		case v1alpha1.BareMetalClusterReasonHostsProgressing:
			reason = v1alpha1.BareMetalClusterReasonProgressing
		case v1alpha1.BareMetalClusterReasonHostsDeleting:
			reason = v1alpha1.BareMetalClusterReasonDeleting
		default:
			reason = v1alpha1.BareMetalClusterReasonFailed
		}

		bareMetalCluster.SetStatusCondition(
			v1alpha1.BareMetalClusterConditionTypeReady,
			status,
			reason,
			hostsReason,
		)

		statusErr := r.Status().Update(ctx, bareMetalCluster)
		if statusErr != nil {
			return result, statusErr
		}
	}

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *BareMetalClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.BareMetalCluster{}).
		Named("baremetalcluster").
		Complete(r)
}

// handleUpdate processes BareMetalCluster creation or specification updates.
func (r *BareMetalClusterReconciler) handleUpdate(ctx context.Context, bareMetalCluster *v1alpha1.BareMetalCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Handling update", "name", bareMetalCluster.Name)

	bareMetalCluster.InitializeStatusConditions()

	if controllerutil.AddFinalizer(bareMetalCluster, BareMetalClusterFinalizer) {
		// Update should fire another reconcile event, so just return
		err := r.Update(ctx, bareMetalCluster)
		return ctrl.Result{}, err
	}

	if bareMetalCluster.Status.MatchType == "" {
		bareMetalCluster.Status.MatchType = bareMetalCluster.Spec.MatchType
	}

	if bareMetalCluster.Status.HostSets == nil {
		bareMetalCluster.Status.HostSets = []v1alpha1.HostSet{}
	}

	/*
		err := r.verifyNetworkHostSets(bareMetalCluster)
		if err != nil {
			log.Error(err, "Failed to verify network host sets")
			return ctrl.Result{}, err
		}
	*/

	// positive delta means add hosts of the HostClass, negative means remove
	// hostClassToHostSetDelta is never written to after init-ing it, so we dont need sync.Map
	hostClassToHostSetDelta := map[string]int{}
	hostClassToCurrentHostSetSize := map[string]int{}
	requiresUpdate := false
	for _, hostSet := range bareMetalCluster.Spec.HostSets {
		hostClassToHostSetDelta[hostSet.HostClass] = hostSet.Size
	}
	for _, hostSet := range bareMetalCluster.Status.HostSets {
		hostClassToHostSetDelta[hostSet.HostClass] -= hostSet.Size
		if hostClassToHostSetDelta[hostSet.HostClass] != 0 {
			requiresUpdate = true
		}
		hostClassToCurrentHostSetSize[hostSet.HostClass] = hostSet.Size
	}

	if !requiresUpdate && len(bareMetalCluster.Status.HostSets) != 0 {
		log.Info("No update required")
		bareMetalCluster.SetStatusCondition(
			v1alpha1.BareMetalClusterConditionTypeHostsReady,
			metav1.ConditionTrue,
			v1alpha1.BareMetalClusterReasonHostsAvailable,
			"Successfully attached/detached all hosts",
		)
		return ctrl.Result{}, nil
	}

	hostClassToAvailableHosts, err := r.verifyAvailableHosts(ctx, bareMetalCluster, hostClassToHostSetDelta)
	if err != nil {
		if err.Error() == "insufficient hosts" {
			log.Info("Insufficient hosts available")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		log.Error(err, "Failed to verify host availability")
		return ctrl.Result{}, err
	}

	// attach or detach hosts to match spec
	// TODO: can use goroutines
	for hostClass, delta := range hostClassToHostSetDelta {
		if delta > 0 {
			hosts := hostClassToAvailableHosts[hostClass]
			err = r.setHostsAttachment(
				ctx,
				bareMetalCluster,
				hosts,
				string(bareMetalCluster.UID),
				hostClassToCurrentHostSetSize,
			)
			if err != nil {
				break
			}
			log.Info(fmt.Sprintf("Attached %d %s hosts to cluster %s", delta, hostClass, string(bareMetalCluster.UID)))
		} else if delta < 0 {
			hosts, err := r.getHosts(
				ctx,
				hostClass,
				-delta,
				bareMetalCluster.Spec.MatchType,
				string(bareMetalCluster.UID),
			)
			if err != nil {
				log.Error(err, "Failed to get hosts from inventory")
				bareMetalCluster.SetStatusCondition(
					v1alpha1.BareMetalClusterConditionTypeHostsReady,
					metav1.ConditionFalse,
					v1alpha1.BareMetalClusterReasonInventoryServiceFailed,
					"Failed to get hosts from inventory",
				)
				break
			}

			err = r.setHostsAttachment(
				ctx,
				bareMetalCluster,
				hosts,
				"",
				hostClassToCurrentHostSetSize,
			)
			if err != nil {
				break
			}
			log.Info(fmt.Sprintf("Detached %d %s hosts from cluster %s", -delta, hostClass, string(bareMetalCluster.UID)))
		}
	}

	// need to update HostSets even on failure
	updatedHostSets := make([]v1alpha1.HostSet, 0, len(hostClassToCurrentHostSetSize))
	for hostClass, size := range hostClassToCurrentHostSetSize {
		updatedHostSets = append(updatedHostSets, v1alpha1.HostSet{
			HostClass: hostClass,
			Size:      size,
		})
	}
	bareMetalCluster.Status.HostSets = updatedHostSets

	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Successfully attached/detached all hosts")
	bareMetalCluster.SetStatusCondition(
		v1alpha1.BareMetalClusterConditionTypeHostsReady,
		metav1.ConditionTrue,
		v1alpha1.BareMetalClusterReasonHostsAvailable,
		"Successfully attached/detached all hosts",
	)

	return ctrl.Result{}, nil
}

// handleDeletion handles the cleanup when a BareMetalCluster is being deleted
// nolint
func (r *BareMetalClusterReconciler) handleDeletion(ctx context.Context, bareMetalCluster *v1alpha1.BareMetalCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Handling delete", "name", bareMetalCluster.Name)

	bareMetalCluster.SetStatusCondition(
		v1alpha1.BareMetalClusterConditionTypeHostsReady,
		metav1.ConditionFalse,
		v1alpha1.BareMetalClusterReasonHostsDeleting,
		"BareMetalCluster's hosts are being freed",
	)

	hostClassToCurrentHostSetSize := map[string]int{}
	for _, hostSet := range bareMetalCluster.Status.HostSets {
		hostClassToCurrentHostSetSize[hostSet.HostClass] = hostSet.Size
	}

	// TODO: can use goroutines
	var err error
	for _, hostSet := range bareMetalCluster.Status.HostSets {
		hosts, err := r.getHosts(
			ctx,
			hostSet.HostClass,
			hostSet.Size,
			bareMetalCluster.Status.MatchType,
			string(bareMetalCluster.UID),
		)
		if err != nil {
			log.Error(err, "Failed to get hosts from inventory during deletion")
			bareMetalCluster.SetStatusCondition(
				v1alpha1.BareMetalClusterConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalClusterReasonInventoryServiceFailed,
				"Failed to get hosts from inventory during deletion",
			)
			break
		}

		err = r.setHostsAttachment(
			ctx,
			bareMetalCluster,
			hosts,
			"",
			hostClassToCurrentHostSetSize,
		)
		if err != nil {
			break
		}
	}

	// need to update HostSets even on failure
	updatedHostSets := make([]v1alpha1.HostSet, 0, len(hostClassToCurrentHostSetSize))
	for hostClass, size := range hostClassToCurrentHostSetSize {
		updatedHostSets = append(updatedHostSets, v1alpha1.HostSet{
			HostClass: hostClass,
			Size:      size,
		})
	}
	bareMetalCluster.Status.HostSets = updatedHostSets

	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Successfully deleted cluster")
	if controllerutil.RemoveFinalizer(bareMetalCluster, BareMetalClusterFinalizer) {
		// Update should fire another reconcile event, so just return
		err := r.Update(ctx, bareMetalCluster)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

/*
func (r *BareMetalClusterReconciler) verifyNetworkHostSets(bareMetalCluster *v1alpha1.BareMetalCluster) error {
	networks := bareMetalCluster.Spec.Networks
	clusterHostSets := map[string]v1alpha1.HostSet{}
	maps.Copy(clusterHostSets, bareMetalCluster.Spec.HostSets)

	for i := range networks {
		for hostClass, networkHostSet := range networks[i].HostSets {
			clusterHostSet := clusterHostSets[hostClass]
			clusterHostSet.Size -= networkHostSet.Size
			clusterHostSets[hostClass] = clusterHostSet
		}
	}

	for hostClass := range clusterHostSets {
		if clusterHostSets[hostClass].Size < 0 {
			bareMetalCluster.SetStatusCondition(
				v1alpha1.BareMetalClusterConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalClusterReasonHostsUnavailable,
				"Failed to verify network host sets",
			)
			return errors.New("network expects more hosts than specified")
		}
	}

	return nil
}
*/

func (r *BareMetalClusterReconciler) verifyAvailableHosts(ctx context.Context, bareMetalCluster *v1alpha1.BareMetalCluster, hostClassToHostSetDelta map[string]int) (map[string][]Host, error) {
	log := logf.FromContext(ctx).V(1)
	ctx = logf.IntoContext(ctx, log)

	hostClassToAvailableHosts := map[string][]Host{}
	// TODO: can use goroutines
	for hostClass, delta := range hostClassToHostSetDelta {
		if delta <= 0 {
			continue
		}

		hosts, err := r.getHosts(
			ctx,
			hostClass,
			delta,
			bareMetalCluster.Spec.MatchType,
			"",
		)
		if err != nil {
			log.Error(err, "Failed to get hosts from inventory")
			bareMetalCluster.SetStatusCondition(
				v1alpha1.BareMetalClusterConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalClusterReasonInventoryServiceFailed,
				"Failed to get hosts from inventory",
			)
			return nil, err
		}
		if len(hosts) < delta {
			err := errors.New("insufficient hosts")
			log.Error(
				err,
				"There are not enough available hosts in the inventory",
				"host class", hostClass,
				"desired additional hosts", delta,
				"current additional hosts", len(hosts),
			)
			bareMetalCluster.SetStatusCondition(
				v1alpha1.BareMetalClusterConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalClusterReasonInsufficientHosts,
				"There are not enough available hosts in the inventory",
			)
			return nil, err
		}

		hostClassToAvailableHosts[hostClass] = append(hostClassToAvailableHosts[hostClass], hosts...)
	}

	log.Info("Successfully verified available hosts")

	return hostClassToAvailableHosts, nil
}

func (r *BareMetalClusterReconciler) getHosts(ctx context.Context, hostClass string, count int, matchType string, clusterId string) ([]Host, error) {
	log := logf.FromContext(ctx).V(1)
	ctx = logf.IntoContext(ctx, log)

	inventoryURL := *r.OsacInventoryUrl
	inventoryURL.Path = "/v1/nodes/detail"
	query := url.Values{}
	// TODO: set query values for pluggable bare metal adapter
	inventoryURL.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, inventoryURL.String(), nil)
	if err != nil {
		log.Error(err, "Failed to create NewRequestWithContext", "method", "getHosts")
		return nil, err
	}

	httpRequest.Header.Set("X-Auth-Token", r.AuthToken)
	httpRequest.Header.Set("X-OpenStack-Ironic-API-Version", "1.69")

	response, err := r.HttpClient.Do(httpRequest)
	if err != nil {
		log.Error(err, "Failed to perform request", "method", "getHosts")
		return nil, err
	}
	defer func() {
		err := response.Body.Close()
		if err != nil {
			log.Error(err, "Failed to close connection", "method", "getHosts")
		}
	}()

	if response.StatusCode != http.StatusOK {
		message, err := io.ReadAll(response.Body)
		if err != nil {
			log.Error(err, "Failed to read response body", "method", "getHosts")
			return nil, err
		}
		err = errors.New(string(message))
		return nil, err
	}

	hostResponse := HostResponse{}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&hostResponse); err != nil {
		log.Error(err, "Failed to decode response body", "method", "getHosts")
		return nil, err
	}

	// assume filters don't work on the inventory
	hosts := []Host{}
	for _, host := range hostResponse.Hosts {
		hostMatchType := ""
		hostClusterId := ""
		if host.Extra != nil {
			if mt, ok := host.Extra["matchType"].(string); ok {
				hostMatchType = mt
			}
			if cid, ok := host.Extra["clusterId"].(string); ok {
				hostClusterId = cid
			}
		}

		if host.HostClass == hostClass &&
			hostMatchType == matchType &&
			hostClusterId == clusterId {
			hosts = append(hosts, host)
		}

		if count > 0 && len(hosts) >= count {
			break
		}
	}

	log.Info("Successfully queried for hosts", "hosts", hosts)

	return hosts, nil
}

func (r *BareMetalClusterReconciler) setHostsAttachment(
	ctx context.Context,
	bareMetalCluster *v1alpha1.BareMetalCluster,
	hosts []Host,
	clusterId string,
	hostClassToCurrentHostSetSize map[string]int,
) error {

	if clusterId == "" {
		for i := range hosts {
			err := r.unmarkAndDetachHost(
				ctx,
				bareMetalCluster,
				hosts[i],
				hostClassToCurrentHostSetSize,
			)
			if err != nil {
				return err
			}
		}
	} else {
		for i := range hosts {
			err := r.markAndAttachHost(
				ctx,
				bareMetalCluster,
				hosts[i],
				clusterId,
				hostClassToCurrentHostSetSize,
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *BareMetalClusterReconciler) markAndAttachHost(
	ctx context.Context,
	bareMetalCluster *v1alpha1.BareMetalCluster,
	host Host,
	clusterId string,
	hostClassToCurrentHostSetSize map[string]int,
) error {
	log := logf.FromContext(ctx).V(1)
	ctx = logf.IntoContext(ctx, log)

	matchType, _ := host.Extra["matchType"].(string)

	// mark inventory host as attached
	hostClass := host.HostClass
	err := r.patchInventoryHostClusterId(ctx, host.NodeId, clusterId)
	if err != nil {
		log.Error(err, "Failed to attach host", "NodeId", host.NodeId)
		bareMetalCluster.SetStatusCondition(
			v1alpha1.BareMetalClusterConditionTypeHostsReady,
			metav1.ConditionFalse,
			v1alpha1.BareMetalClusterReasonInventoryServiceFailed,
			"Failed to attach some hosts",
		)
		return err
	}
	hostClassToCurrentHostSetSize[hostClass] += 1

	// create Host CR
	existingHosts := &v1alpha1.TestHostList{}
	err = r.List(
		ctx,
		existingHosts,
		client.InNamespace(bareMetalCluster.Namespace),
		client.MatchingLabels{
			"osac.openshift.io/node-id": host.NodeId,
		},
	)
	if len(existingHosts.Items) == 0 && client.IgnoreNotFound(err) == nil {
		// Host doesn't exist so we create it
		hostName := fmt.Sprintf("host-%s-%s", host.HostClass, rand.String(8))
		hostCR := &v1alpha1.TestHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hostName,
				Namespace: bareMetalCluster.Namespace,
				Labels: client.MatchingLabels{
					"osac.openshift.io/cluster-id": string(bareMetalCluster.UID),
					"osac.openshift.io/node-id":    host.NodeId,
					"osac.openshift.io/host-class": host.HostClass,
				},
			},
			Spec: v1alpha1.TestHostSpec{
				NodeId:    host.NodeId,
				MatchType: matchType,
				HostClass: host.HostClass,
				Online:    false,
			},
		}

		err := controllerutil.SetControllerReference(bareMetalCluster, hostCR, r.Scheme)
		if err != nil {
			log.Error(err, "Failed to set controller reference to Host CR", "NodeId", host.NodeId)
			bareMetalCluster.SetStatusCondition(
				v1alpha1.BareMetalClusterConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalClusterReasonHostOperationFailed,
				"Failed to set controller reference to Host CR",
			)
		}

		err = r.Create(ctx, hostCR)
		if err != nil {
			log.Error(err, "Failed to create Host CR", "NodeId", host.NodeId)
			bareMetalCluster.SetStatusCondition(
				v1alpha1.BareMetalClusterConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.BareMetalClusterReasonHostOperationFailed,
				"Failed to create Host CR",
			)
			return err
		}
	} else if len(existingHosts.Items) > 0 {
		err = errors.New("Host CR already exists")
		log.Error(err, "Failed to create Host CR", "NodeId", host.NodeId)
		return err
	} else {
		// Unexpected error
		log.Error(err, "Failed to get Host CR", "NodeId", host.NodeId)
		bareMetalCluster.SetStatusCondition(
			v1alpha1.BareMetalClusterConditionTypeHostsReady,
			metav1.ConditionFalse,
			v1alpha1.BareMetalClusterReasonHostsUnavailable,
			"Failed to create Host CR",
		)
		return err
	}

	log.Info("Successfully attached host", "NodeId", host.NodeId)

	return nil
}

func (r *BareMetalClusterReconciler) unmarkAndDetachHost(
	ctx context.Context,
	bareMetalCluster *v1alpha1.BareMetalCluster,
	host Host,
	hostClassToCurrentHostSetSize map[string]int,
) error {
	log := logf.FromContext(ctx).V(1)
	ctx = logf.IntoContext(ctx, log)

	// delete Host CRs
	err := r.DeleteAllOf(
		ctx,
		&v1alpha1.TestHost{},
		client.InNamespace(bareMetalCluster.Namespace),
		client.MatchingLabels{
			"osac.openshift.io/cluster-id": string(bareMetalCluster.UID),
			"osac.openshift.io/node-id":    host.NodeId,
		},
	)
	if client.IgnoreNotFound(err) != nil {
		log.Error(err, "Failed to delete Host CR", "NodeId", host.NodeId)
		bareMetalCluster.SetStatusCondition(
			v1alpha1.BareMetalClusterConditionTypeHostsReady,
			metav1.ConditionFalse,
			v1alpha1.BareMetalClusterReasonHostOperationFailed,
			"Failed to delete Host CR",
		)
		return err
	}

	// mark inventory host as detached
	hostClass := host.HostClass
	err = r.patchInventoryHostClusterId(ctx, host.NodeId, "")
	if err != nil {
		log.Error(err, "Failed to free host", "NodeId", host.NodeId)
		bareMetalCluster.SetStatusCondition(
			v1alpha1.BareMetalClusterConditionTypeHostsReady,
			metav1.ConditionFalse,
			v1alpha1.BareMetalClusterReasonInventoryServiceFailed,
			"Failed to free some hosts",
		)
		return err
	}
	hostClassToCurrentHostSetSize[hostClass] -= 1
	if hostClassToCurrentHostSetSize[hostClass] == 0 {
		delete(hostClassToCurrentHostSetSize, hostClass)
	}

	log.Info("Successfully detached host", "NodeId", host.NodeId)

	return nil
}

func (r *BareMetalClusterReconciler) patchInventoryHostClusterId(ctx context.Context, nodeId string, clusterId string) error {
	log := logf.FromContext(ctx).V(1)

	managementURL := *r.OsacManagementUrl
	managementURL.Path = "/v1/nodes/" + nodeId

	patchBody := []map[string]string{
		{
			"op":    "replace",
			"path":  "/extra/clusterId",
			"value": clusterId,
		},
	}

	bodyBytes, err := json.Marshal(patchBody)
	if err != nil {
		log.Error(err, "Failed to marshal request body", "method", "patchInventoryHostClusterId")
		return err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPatch, managementURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		log.Error(err, "Failed to create NewRequestWithContext", "method", "patchInventoryHostClusterId")
		return err
	}

	httpRequest.Header.Set("X-Auth-Token", r.AuthToken)
	httpRequest.Header.Set("X-OpenStack-Ironic-API-Version", "1.69")
	httpRequest.Header.Set("Content-Type", "application/json-patch+json")

	response, err := r.HttpClient.Do(httpRequest)
	if err != nil {
		log.Error(err, "Failed to perform request", "method", "patchInventoryHostClusterId")
		return err
	}
	defer func() {
		err := response.Body.Close()
		if err != nil {
			log.Error(err, "Failed to close connection", "method", "patchInventoryHostClusterId")
		}
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, err := io.ReadAll(response.Body)
		if err != nil {
			log.Error(err, "Failed to read response body", "method", "patchInventoryHostClusterId")
			return err
		}
		err = errors.New(string(message))
		return err
	}

	log.Info("Successfully patched host", "host", nodeId)

	return nil
}
