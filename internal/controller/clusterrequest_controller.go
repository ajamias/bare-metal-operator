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
	"context"
	"maps"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
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
	Scheme *runtime.Scheme
}

const ClusterRequestFinalizer = "cloudkit.openshift.io/cluster-request"

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

	if err == nil {
		r.updateReadyCondition(clusterRequest)

		if !equality.Semantic.DeepEqual(clusterRequest.Status, *oldstatus) {
			log.Info("status requires update")
			if statusErr := r.Status().Update(ctx, clusterRequest); statusErr != nil {
				return result, statusErr
			}
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

// handleUpdate processes ClusterRequest creation or specification updates
// nolint
func (r *ClusterRequestReconciler) handleUpdate(ctx context.Context, clusterRequest *v1alpha1.ClusterRequest) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Updating ClusterRequest", "name", clusterRequest.Name)

	clusterRequest.InitializeStatusConditions()

	if controllerutil.AddFinalizer(clusterRequest, ClusterRequestFinalizer) {
		if err := r.Update(ctx, clusterRequest); err != nil {
			return ctrl.Result{}, err
		}
	}

	if clusterRequest.Status.MatchType == "" {
		clusterRequest.Status.MatchType = clusterRequest.Spec.MatchType
	}

	if clusterRequest.Status.HostSets == nil {
		clusterRequest.Status.HostSets = make(map[string]v1alpha1.HostSet, 0)
	}

	if clusterRequest.Status.MatchType != clusterRequest.Spec.MatchType {
		log.Info("Current MatchType is different from specified one")

		log.Info("TODO: get Host info associated with this cluster from either Host CRs or BM Inventory")
		log.Info("TODO: for each host.Type != MatchType, free it and update the ClusterRequest's HostSet")
	}

	if !maps.Equal(clusterRequest.Status.HostSets, clusterRequest.Spec.HostSets) {
		log.Info("Current HostSets are different from specified ones")

		log.Info("TODO: get Host info with matching MatchType from either Host CRs or BM Inventory")
		log.Info("TODO: if we need to add but there are not enough available Hosts, requeue reconcile ")
		log.Info("TODO: for each host, mark it used/freed by this ClusterRequest and update the HostSet")

		// for now, just make them equal
		clusterRequest.Status.HostSets = clusterRequest.Spec.HostSets
	}

	// check on hosts, if not all hosts are ready, requeue reconcile

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

	if controllerutil.RemoveFinalizer(clusterRequest, ClusterRequestFinalizer) {
		if err := r.Update(ctx, clusterRequest); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// updateReadyCondition updates the Ready condition based on the HostsReady condition
func (r *ClusterRequestReconciler) updateReadyCondition(clusterRequest *v1alpha1.ClusterRequest) {
	hostsCondition := meta.FindStatusCondition(clusterRequest.Status.Conditions, v1alpha1.ClusterRequestConditionTypeHostsReady)

	readyStatus := metav1.ConditionFalse
	readyReason := v1alpha1.ClusterRequestReasonFailed

	if hostsCondition != nil {
		switch hostsCondition.Reason {
		case v1alpha1.ClusterRequestReasonHostsAvailable:
			readyStatus = metav1.ConditionTrue
			readyReason = v1alpha1.ClusterRequestReasonReady
		case v1alpha1.ClusterRequestReasonHostsAllocating:
			readyReason = v1alpha1.ClusterRequestReasonProgressing
		case v1alpha1.ClusterRequestReasonHostsFreeing, v1alpha1.ClusterRequestReasonHostsFreed:
			readyReason = v1alpha1.ClusterRequestReasonDeleting
		default:
			readyReason = v1alpha1.ClusterRequestReasonFailed
		}
	}

	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeReady,
		readyStatus,
		readyReason,
		readyReason,
	)
}
