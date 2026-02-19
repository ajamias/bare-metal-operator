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
	"maps"
	"net/http"
	"net/url"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/ajamias/bare-metal-operator/api/v1alpha1"
)

// ClusterRequestReconciler reconciles a ClusterRequest object
type ClusterRequestReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	OsacInventoryUrl  *url.URL
	OsacManagementUrl *url.URL
	AuthToken         string
}

const ClusterRequestFinalizer = "cloudkit.openshift.io/cluster-request"

// HostResponse represents the response from the inventory service
type HostResponse struct {
	Hosts []Host `json:"nodes"`
}

// Host represents a single host from the inventory
type Host struct {
	NodeId         string         `json:"uuid"`
	HostClass      string         `json:"resource_class"`
	ProvisionState string         `json:"provision_state"` // available, active, etc
	Extra          map[string]any `json:"extra"`           // contains matchType and clusterId
}

// +kubebuilder:rbac:groups=cloudkit.openshift.io,resources=clusterrequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudkit.openshift.io,resources=clusterrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudkit.openshift.io,resources=clusterrequests/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	clusterRequest := &v1alpha1.ClusterRequest{}
	err := r.Get(ctx, req.NamespacedName, clusterRequest)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Info("Starting reconcile", "namespacedName", req.NamespacedName, "name", clusterRequest.Name)

	oldstatus := clusterRequest.Status.DeepCopy()

	var result ctrl.Result
	if !clusterRequest.DeletionTimestamp.IsZero() {
		result, err = r.handleDeletion(ctx, clusterRequest)
	} else {
		result, err = r.handleUpdate(ctx, clusterRequest)
	}

	if !equality.Semantic.DeepEqual(clusterRequest.Status, *oldstatus) {
		log.Info("status requires update")
		if statusErr := r.Status().Update(ctx, clusterRequest); statusErr != nil {
			return result, statusErr
		}
	}

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterRequest{}).
		Named("clusterrequest").
		Complete(r)
}

/*
	TODO: Some design decisions to consider:
	1. Do we want operations to be as atomic as possible?
	   i.e. if the user wants to switch matchType and not all hosts can be
	   freed, do we still allocate hosts of the other matchType?
	2. Do we want operations to fail as fast as possible?
	   i.e if one of the hosts cannot be freed/allocated, do we immediately
	   return and re-reconcile or try to free/allocate the rest?
*/

// handleUpdate processes ClusterRequest creation or specification updates
// nolint
func (r *ClusterRequestReconciler) handleUpdate(ctx context.Context, clusterRequest *v1alpha1.ClusterRequest) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Updating ClusterRequest", "name", clusterRequest.Name)

	clusterRequest.InitializeStatusConditions()

	if controllerutil.AddFinalizer(clusterRequest, ClusterRequestFinalizer) {
		// Update should fire another reconcile event, so just return
		err := r.Update(ctx, clusterRequest)
		return ctrl.Result{}, err
	}

	if clusterRequest.Status.MatchType == "" {
		clusterRequest.Status.MatchType = clusterRequest.Spec.MatchType
	}

	if clusterRequest.Status.HostSets == nil {
		clusterRequest.Status.HostSets = make(map[string]v1alpha1.HostSet, 0)
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	log.Info("Checking for available target hosts")
	hostClassToHosts := map[string][]Host{}
	// TODO: can use goroutines
	for hostClass, hostSet := range clusterRequest.Spec.HostSets {
		if hostSet.Size <= clusterRequest.Status.HostSets[hostClass].Size {
			continue
		}

		hosts, err := r.getHosts(ctx, httpClient, hostClass, hostSet.Size, clusterRequest.Spec.MatchType, "")
		if err != nil {
			log.Error(err, "Failed to get hosts from inventory")
			clusterRequest.SetStatusCondition(
				v1alpha1.ClusterRequestConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.ClusterRequestReasonHostsUnavailable,
				"Failed to get hosts from inventory",
			)
			return ctrl.Result{}, err
		}
		if len(hosts) < hostSet.Size {
			err := errors.New("Insufficient hosts")
			log.Error(
				err,
				"There are not enough available hosts in the inventory",
				"host class", hostClass,
				"target size", hostSet.Size,
				"actual size", len(hosts),
			)
			clusterRequest.SetStatusCondition(
				v1alpha1.ClusterRequestConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.ClusterRequestReasonInsufficientHosts,
				"There are not enough available hosts in the inventory",
			)
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, err
		}

		hostClassToHosts[hostClass] = append(hostClassToHosts[hostClass], hosts...)
	}

	if clusterRequest.Status.MatchType != clusterRequest.Spec.MatchType {
		log.Info("Current MatchType is different from specified one")

		// free attached hosts
		// TODO: can use goroutines
		for hostClass, hostSet := range clusterRequest.Status.HostSets {
			log.Info("List Host info from inventory")
			hosts, err := r.getHosts(ctx, httpClient, hostClass, hostSet.Size, clusterRequest.Status.MatchType, string(clusterRequest.UID))
			if err != nil {
				log.Error(err, "Failed to get hosts from inventory")
				clusterRequest.SetStatusCondition(
					v1alpha1.ClusterRequestConditionTypeHostsReady,
					metav1.ConditionFalse,
					v1alpha1.ClusterRequestReasonHostsUnavailable,
					"Failed to get hosts from inventory",
				)
				return ctrl.Result{}, err
			}

			for i := range hosts {
				log.Info("Free host", "node id", hosts[i].NodeId)
				err := r.setHostAttachment(ctx, httpClient, hosts[i].NodeId, "")
				if err != nil {
					log.Error(err, "Failed to free host", "node id", hosts[i].NodeId)
					clusterRequest.SetStatusCondition(
						v1alpha1.ClusterRequestConditionTypeHostsReady,
						metav1.ConditionFalse,
						v1alpha1.ClusterRequestReasonHostsUnavailable,
						"Failed to free some hosts",
					)
					return ctrl.Result{}, err
				}
				clusterRequest.Status.HostSets[hostClass] = v1alpha1.HostSet{
					Size: clusterRequest.Status.HostSets[hostClass].Size - 1,
				}
			}
		}

		clusterRequest.Status.MatchType = clusterRequest.Spec.MatchType
	}

	if !maps.Equal(clusterRequest.Status.HostSets, clusterRequest.Spec.HostSets) {
		log.Info("Current HostSets are different from specified ones")

		// TODO: can use goroutines
		for hostClass, hostSet := range clusterRequest.Spec.HostSets {
			desiredSize := hostSet.Size
			actualSize := clusterRequest.Status.HostSets[hostClass].Size
			sizeDifference := desiredSize - actualSize
			hosts := hostClassToHosts[hostClass]

			if sizeDifference == 0 {
				continue
			}

			var (
				op        string
				clusterId string
				delta     int
			)
			if sizeDifference > 0 && sizeDifference == len(hosts) {
				op = "add"
				clusterId = string(clusterRequest.UID)
				delta = 1
			} else if sizeDifference < 0 && -sizeDifference == len(hosts) {
				op = "free"
				clusterId = ""
				delta = -1
			} else {
				err := errors.New("Something went wrong")
				log.Error(err, "Fail")
				return ctrl.Result{}, err
			}

			for i := range hosts {
				err := r.setHostAttachment(ctx, httpClient, hosts[i].NodeId, clusterId)
				if err != nil {
					log.Error(err, "Failed to "+op+" host", "node id", hosts[i].NodeId)
					clusterRequest.SetStatusCondition(
						v1alpha1.ClusterRequestConditionTypeHostsReady,
						metav1.ConditionFalse,
						v1alpha1.ClusterRequestReasonHostsUnavailable,
						"Failed to "+op+" some hosts",
					)
					return ctrl.Result{}, err
				}
				clusterRequest.Status.HostSets[hostClass] = v1alpha1.HostSet{
					Size: clusterRequest.Status.HostSets[hostClass].Size + delta,
				}
				log.Info("Succeeded to "+op+" host", "node id", hosts[i].NodeId)
			}
		}

		clusterRequest.SetStatusCondition(
			v1alpha1.ClusterRequestConditionTypeHostsReady,
			metav1.ConditionFalse,
			v1alpha1.ClusterRequestReasonHostsAllocating,
			"Successfully reserved hosts",
		)
	}

	log.Info("Check on hosts, if not all hosts are ready, requeue reconcile")
	for hostClass, hostSet := range clusterRequest.Spec.HostSets {
		hosts, err := r.getHosts(ctx, httpClient, hostClass, hostSet.Size, clusterRequest.Spec.MatchType, string(clusterRequest.UID))
		if err != nil {
			log.Error(err, "Failed to get hosts from inventory")
			clusterRequest.SetStatusCondition(
				v1alpha1.ClusterRequestConditionTypeHostsReady,
				metav1.ConditionFalse,
				v1alpha1.ClusterRequestReasonHostsUnavailable,
				"Failed to get hosts from inventory",
			)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}

		for i := range hosts {
			if hosts[i].ProvisionState != "available" {
				err := errors.New("Hosts unavailable")
				log.Error(err, "Not all hosts are ready yet")
				clusterRequest.SetStatusCondition(
					v1alpha1.ClusterRequestConditionTypeHostsReady,
					metav1.ConditionFalse,
					v1alpha1.ClusterRequestReasonHostsUnavailable,
					"Failed to verify that all hosts are ready",
				)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
		}
	}

	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeHostsReady,
		metav1.ConditionTrue,
		v1alpha1.ClusterRequestReasonHostsAvailable,
		"All Hosts are available",
	)

	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeReady,
		metav1.ConditionTrue,
		v1alpha1.ClusterRequestReasonReady,
		"All Hosts are available",
	)

	return ctrl.Result{}, nil
}

// handleDeletion handles the cleanup when a ClusterRequest is being deleted
// nolint
func (r *ClusterRequestReconciler) handleDeletion(ctx context.Context, clusterRequest *v1alpha1.ClusterRequest) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Deleting ClusterRequest", "name", clusterRequest.Name)

	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeReady,
		metav1.ConditionFalse,
		v1alpha1.ClusterRequestReasonDeleting,
		"ClusterRequest is being deleted",
	)
	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeHostsReady,
		metav1.ConditionFalse,
		v1alpha1.ClusterRequestReasonHostsFreeing,
		"ClusterRequest's hosts are being freed",
	)

	for hostClass, hostSet := range clusterRequest.Status.HostSets {
		// for now, just delete
		for _ = range hostSet.Size {
			log.Info("TODO: mark host as freed, let Host operator take care of Host CR deletion, might update need to update BM inventory", "hostClass", hostClass)
			log.Info("TODO: if error occurs, update status and requeue reconcile")
			clusterRequest.Status.HostSets[hostClass] = v1alpha1.HostSet{
				Size: clusterRequest.Status.HostSets[hostClass].Size - 1,
			}
		}
		if clusterRequest.Status.HostSets[hostClass].Size == 0 {
			delete(clusterRequest.Status.HostSets, hostClass)
		}
	}

	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeHostsReady,
		metav1.ConditionFalse,
		v1alpha1.ClusterRequestReasonHostsFreed,
		"ClusterRequest's hosts are now free",
	)

	// At this point, all underlying infrastructure is freed from
	// the ClusterRequest, so just return
	if controllerutil.RemoveFinalizer(clusterRequest, ClusterRequestFinalizer) {
		// Update should fire another reconcile event, so just return
		err := r.Update(ctx, clusterRequest)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ClusterRequestReconciler) getHosts(ctx context.Context, httpClient *http.Client, hostClass string, _ int, matchType string, clusterId string) ([]Host, error) {
	log := logf.FromContext(ctx)

	inventoryURL := *r.OsacInventoryUrl
	inventoryURL.Path = "/v1/nodes/detail"
	query := url.Values{}
	// TODO: set query values for pluggable bare metal adapter
	inventoryURL.RawQuery = query.Encode()

	httpRequest, err := http.NewRequest(http.MethodGet, inventoryURL.String(), nil)
	if err != nil {
		return nil, err
	}

	httpRequest.Header.Set("X-Auth-Token", r.AuthToken)
	httpRequest.Header.Set("X-OpenStack-Ironic-API-Version", "1.69")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
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
			return nil, err
		}
		err = errors.New(string(message))
		return nil, err
	}

	hostResponse := HostResponse{}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&hostResponse); err != nil {
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
	}

	log.Info("Successfully queried for hosts", "hosts", hosts)

	return hosts, nil
}

func (r *ClusterRequestReconciler) setHostAttachment(ctx context.Context, httpClient *http.Client, nodeId string, clusterId string) error {
	log := logf.FromContext(ctx)

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
		return fmt.Errorf("failed to marshal patch body: %w", err)
	}

	httpRequest, err := http.NewRequest(http.MethodPatch, managementURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create PATCH request: %w", err)
	}

	httpRequest.Header.Set("X-Auth-Token", r.AuthToken)
	httpRequest.Header.Set("X-OpenStack-Ironic-API-Version", "1.69")
	httpRequest.Header.Set("Content-Type", "application/json-patch+json")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("failed to execute PATCH request: %w", err)
	}
	defer func() {
		err := response.Body.Close()
		if err != nil {
			log.Error(err, "Failed to close connection", "method", "patchHost")
		}
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("PATCH request failed with status %d", response.StatusCode)
	}

	log.Info("Successfully patched host", "host", nodeId)

	return nil
}
