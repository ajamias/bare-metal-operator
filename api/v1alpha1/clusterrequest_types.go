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

// ClusterRequestSpec defines the desired state of ClusterRequest.
type ClusterRequestSpec struct {
	// MatchType specifies the criteria for selecting hosts
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Bare;Agent;Virtual
	// +kubebuilder:default=Bare
	MatchType string `json:"matchType"`

	// HostSets defines the number of hosts needed for each host set type.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.all(k, v, k == v.hostClass)"
	HostSets map[string]HostSet `json:"hostSets"`

	// Networks defines the vlans that connect hosts together
	// +kubebuilder:validation:Required
	Networks []Network `json:"networks"`
}

// ClusterRequestStatus defines the observed state of ClusterRequest.
type ClusterRequestStatus struct {
	// MatchType specifies the criteria for selecting hosts
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Bare;Agent;Virtual
	// +kubebuilder:default=Bare
	MatchType string `json:"matchType"`

	// HostSets shows the current allocation of hosts
	// +kubebuilder:validation:Required
	HostSets map[string]HostSet `json:"hostSets"`

	// LastUpdated is the timestamp when the status was last updated
	// +kubebuilder:validation:Optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// Conditions represent the latest available observations of the ClusterRequest state
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// HostSet defines a set of hosts with the same class and required count
type HostSet struct {
	// +kubebuilder:
	HostClass string `json:"hostClass"`

	// Size specifies the number of hosts required for this host class
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	Size int `json:"size"`
}

// Network defines a set of hosts to be put on the same vlan
type Network struct {
	// Name is the name of the network
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// VLAN is the ID of the network
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4094
	VLAN int `json:"vlan"`

	// HostSets specifies the hosts that should be connected to this network
	// +kubebuilder:validation:Required
	HostSets map[string]HostSet `json:"hostSets"`
}

type HostSpec struct {
	// rule="self == oldSelf",message="field is immutable"
	NodeId string `json:"nodeId"`

	// rule="self == oldSelf",message="field is immutable"
	MatchType string `json:"matchType"`

	// rule="self == oldSelf",message="field is immutable"
	HostClass string `json:"hostClass"`

	Online bool `json:"online"`
}

type Host struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HostSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cr;creq
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type == 'Ready')].reason"
// +kubebuilder:printcolumn:name="Hosts Status",type="string",JSONPath=".status.conditions[?(@.type == 'HostsReady')].reason"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.matchType"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ClusterRequest is the Schema for the clusterrequests API.
type ClusterRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterRequestSpec   `json:"spec,omitempty"`
	Status ClusterRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterRequestList contains a list of ClusterRequest.
type ClusterRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterRequest{}, &ClusterRequestList{})
}
