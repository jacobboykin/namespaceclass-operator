/*
Copyright 2025.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NamespaceClassBindingSpec defines the desired state of NamespaceClassBinding
type NamespaceClassBindingSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// foo is an example field of NamespaceClassBinding. Edit namespaceclassbinding_types.go to remove/update
	// +optional
	ClassName string `json:"className"`
}

// NamespaceClassBindingStatus defines the observed state of NamespaceClassBinding.
type NamespaceClassBindingStatus struct {
	// ObservedClassName is the name of the NamespaceClass that was last processed
	// +optional
	ObservedClassName string `json:"observedClassName,omitempty"`

	// ObservedClassGeneration is the generation of the NamespaceClass that was last processed
	// +optional
	ObservedClassGeneration int64 `json:"observedClassGeneration,omitempty"`

	// AppliedResources tracks which resources have been created
	// +optional
	AppliedResources []AppliedResource `json:"appliedResources,omitempty"`

	// conditions represent the current state of the NamespaceClassBinding resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AppliedResource tracks a resource that was applied to the namespace
type AppliedResource struct {
	// APIVersion of the resource
	APIVersion string `json:"apiVersion"`
	// Kind of the resource
	Kind string `json:"kind"`
	// Name of the resource
	Name string `json:"name"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NamespaceClassBinding is the Schema for the namespaceclassbindings API
type NamespaceClassBinding struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of NamespaceClassBinding
	// +required
	Spec NamespaceClassBindingSpec `json:"spec"`

	// status defines the observed state of NamespaceClassBinding
	// +optional
	Status NamespaceClassBindingStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=ncb

// NamespaceClassBindingList contains a list of NamespaceClassBinding
type NamespaceClassBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceClassBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NamespaceClassBinding{}, &NamespaceClassBindingList{})
}
