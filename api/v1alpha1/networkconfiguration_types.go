// Copyright 2024 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NetworkClusterPolicySpec defines the desired state of NetworkClusterPolicy
type NetworkClusterPolicySpec struct {
	// Configuration type that the operator will configure to the nodes. Possible options: gaudi-so.
	// TODO: plausible other options: host-nic
	// +kubebuilder:validation:Enum=gaudi-so
	ConfigurationType string `json:"configurationType"`

	// Select which nodes the operator should target. Align with labels created by NFD.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:items:MinItems=1
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Gaudi Scale-Out specific settings. Only valid when configuration type is 'gaudi-so'
	GaudiScaleOut GaudiScaleOutSpec `json:"gaudiScaleOut,omitempty"`

	// LogLevel sets the operator's log level.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=8
	LogLevel int `json:"logLevel,omitempty"`
}

// GaudiScaleOutSpec defines the desired state of GaudiScaleOut
type GaudiScaleOutSpec struct {
	// Disable Gaudi scale-out interfaces in NetworkManager. For nodes where NetworkManager tries
	// to configure the Gaudi interfaces, prevent it from doing so.
	DisableNetworkManager bool `json:"disableNetworkManager,omitempty"`

	// Layer where the configuration should occur. Possible options: L2 and L3.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=L2;L3
	Layer string `json:"layer,omitempty"`

	// Container image to handle interface configurations on the worker nodes.
	Image string `json:"image,omitempty"`

	// Normal image pull policy used in the resulting daemonset.
	// +kubebuilder:validation:Enum=Never;Always;IfNotPresent
	PullPolicy string `json:"pullPolicy,omitempty"`

	// MTU for the scale-out interfaces.
	// +kubebuilder:validation:Minimum=1500
	// +kubebuilder:validation:Maximum=9000
	MTU int `json:"mtu,omitempty"`

	// Enable LLDP for Priority Flow Control in a dedicated container
	// Keep this value as false if lldpad LLDP daemon is already present and
	// running on the host
	EnableLLDPAD bool `json:"enableLLDPAD,omitempty"`

	// Bitmask of Priority Flow Control priorities to enable
	// Requires lldpad on the host, or enabling the above EnableLLDPAD boolean
	// Currently accepted values are 00000000 or 11110000
	// +kubebuilder:validation:Enum="00000000";"11110000"
	PFCPriorities string `json:"pfcPriorities,omitempty"`
}

// NetworkClusterPolicyStatus defines the observed state of NetworkClusterPolicy
type NetworkClusterPolicyStatus struct {
	Targets    int32    `json:"targets"`
	ReadyNodes int32    `json:"ready"`
	State      string   `json:"state"`
	Errors     []string `json:"errors"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=networkclusterpolicies,scope=Cluster
//+kubebuilder:subresource:status

// NetworkClusterPolicy is the Schema for the networkclusterpolicies API
type NetworkClusterPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkClusterPolicySpec   `json:"spec,omitempty"`
	Status NetworkClusterPolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NetworkClusterPolicyList contains a list of NetworkClusterPolicy
type NetworkClusterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkClusterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkClusterPolicy{}, &NetworkClusterPolicyList{})
}
