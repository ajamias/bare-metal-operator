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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BareMetalPoolSpec defines the desired state of BareMetalPool.
type BareMetalPoolSpec struct {
	// HostSets defines the number of hosts needed for each host set type.
	// The map key is the HostClass and the value is the replicas count.
	// +kubebuilder:validation:Required
	HostSets map[string]int `json:"hostSets"`

	// Profile specifies the workflow to use for additional Host and Cluster configuration
	// +kubebuilder:validation:Optional
	Profile ProfileSpec `json:"profile,omitempty"`
}

// BareMetalPoolStatus defines the observed state of BareMetalPool.
type BareMetalPoolStatus struct {
	// HostSets shows the current allocation of hosts
	// The map key is the HostClass and the value is the replicas count.
	// +kubebuilder:validation:Required
	HostSets map[string]int `json:"hostSets"`

	// SetupWorkflow shows the status and output of the workflow to set up the pool
	// +kubebuilder:validation:Optional
	SetupWorkflow WorkflowStatus `json:"setupWorkflow,omitempty"`

	// LastUpdated is the timestamp when the status was last updated
	// +kubebuilder:validation:Optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// Conditions represent the latest available observations of the BareMetalPool state
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

type ProfileSpec struct {
	// Name is the name of the workflow
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Input is a key value map of inputs for the specified workflow
	// +kubebuilder:validation:Optional
	Input map[string]string `json:"input,omitempty"`
}

type WorkflowStatus struct {
	Status string            `json:"status"`
	Output map[string]string `json:"output"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=bmp;bmpool
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type == 'Ready')].reason"
// +kubebuilder:printcolumn:name="Hosts Status",type="string",JSONPath=".status.conditions[?(@.type == 'HostsReady')].reason"
// +kubebuilder:printcolumn:name="Profile",type="string",JSONPath=".spec.profile.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// BareMetalPool is the Schema for the baremetalpools API.
type BareMetalPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BareMetalPoolSpec   `json:"spec,omitempty"`
	Status BareMetalPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BareMetalPoolList contains a list of BareMetalPool.
type BareMetalPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BareMetalPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BareMetalPool{}, &BareMetalPoolList{})
}
