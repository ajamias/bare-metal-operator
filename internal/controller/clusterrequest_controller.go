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
	"slices"
	"time"

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

	log.Info("Starting reconcile for ClusterRequest", "namespacedName", req.NamespacedName)

	clusterRequest := &v1alpha1.ClusterRequest{}
	err := r.Get(ctx, req.NamespacedName, clusterRequest)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	oldstatus := clusterRequest.Status.DeepCopy()

	var result ctrl.Result
	if !clusterRequest.DeletionTimestamp.IsZero() {
		log.Info("Handling deletion")
		result, err = r.handleDeletion(ctx, clusterRequest)
	} else if clusterRequest.Status.ObservedGeneration != clusterRequest.Generation {
		log.Info("Handling update")
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
// nolint:unparam
func (r *ClusterRequestReconciler) handleUpdate(ctx context.Context, clusterRequest *v1alpha1.ClusterRequest) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	clusterRequest.InitializeStatusConditions()

	if clusterRequest.Status.MatchType == "" {
		clusterRequest.Status.MatchType = clusterRequest.Spec.MatchType
	}

	if clusterRequest.Status.HostSets == nil {
		clusterRequest.Status.HostSets = make([]v1alpha1.HostSet, 0)
	}

	if clusterRequest.Status.MatchType != clusterRequest.Spec.MatchType {
		log.Info("Current MatchType is different from specified one")

		log.Info("TODO: get Host info associated with this cluster from either Host CRs or BM Inventory")
		log.Info("TODO: for each host.Type != MatchType, free it and update the ClusterRequest's HostSet")
	}

	if !slices.Equal(clusterRequest.Status.HostSets, clusterRequest.Spec.HostSets) {
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
		"Hosts are all available",
	)

	return ctrl.Result{}, nil
}

// handleDeletion handles the cleanup when a ClusterRequest is being deleted
func (r *ClusterRequestReconciler) handleDeletion(ctx context.Context, clusterRequest *v1alpha1.ClusterRequest) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info("Processing ClusterRequest deletion", "name", clusterRequest.Name)
	clusterRequest.SetStatusCondition(
		v1alpha1.ClusterRequestConditionTypeReady,
		metav1.ConditionFalse,
		v1alpha1.ClusterRequestReasonDeleting,
		"ClusterRequest is being deleted",
	)

	hostsCondition := meta.FindStatusCondition(clusterRequest.Status.Conditions, v1alpha1.ClusterRequestConditionTypeHostsReady)

	if hostsCondition.Reason == v1alpha1.ClusterRequestReasonHostsFreed {
		if controllerutil.ContainsFinalizer(clusterRequest, ClusterRequestFinalizer) {
			if controllerutil.RemoveFinalizer(clusterRequest, ClusterRequestFinalizer) {
				if err := r.Update(ctx, clusterRequest); err != nil {
					log.Error(err, "Failed to remove finalizer")
					return ctrl.Result{}, err
				}
			}
		}
	} else if hostsCondition.Reason != v1alpha1.ClusterRequestReasonHostsFreeing {
		// TODO: here, I would set each Host CR associated with this ClusterRequest to free itself
		//	 but for now I will log it
		log.Info("TODO: now I will free all the hosts associated with this ClusterRequest")

		log.Info("Setting HostsReady to False because HostsFreeing")
		clusterRequest.SetStatusCondition(
			v1alpha1.ClusterRequestConditionTypeHostsReady,
			metav1.ConditionFalse,
			v1alpha1.ClusterRequestReasonHostsFreeing,
			"Freeing allocated hosts for cluster deletion",
		)

		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	log.Info("Successfully processed ClusterRequest deletion", "name", clusterRequest.Name)
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

	clusterRequest.Status.ObservedGeneration = clusterRequest.Generation
}
