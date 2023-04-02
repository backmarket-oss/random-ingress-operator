/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RandomIngressSpec defines the desired state of RandomIngress
type RandomIngressSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	IngressTemplate IngressTemplateSpec `json:"ingressTemplate"`
}

// IngressTemplate defines the template that should be used to instantiate the Ingress resource.
type IngressTemplateSpec struct {
	// Metadata to add to the ingresses created from this template.
	// +optional
	Metadata IngressTemplateMetadata `json:"metadata,omitempty"`

	// Specification of the desired Ingress object to instantiate.
	// +optional
	Spec networkingv1.IngressSpec `json:"spec,omitempty"`
}

// IngressTemplateMetadata defines the metadata that should be added to the instantiated Ingress resources.
// It only contains vetted fields of metadata: the other usual fields are managed by the RandomIngress operator.
type IngressTemplateMetadata struct {
	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// RandomIngressStatus defines the observed state of RandomIngress
type RandomIngressStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Represents the latest available observations of a randomingress's current state.
	// +optional
	Conditions []RandomIngressCondition `json:"conditions,omitempty"`

	// NextRenewalTime tells the latest time at which the controller will delete the managed Ingress
	// and create a new one with a new random part.
	// +optional
	NextRenewalTime *metav1.Time `json:"nextRenewalTime,omitempty"`
}

// RandomIngressCondition represents an observation on the current state of the randomingress.
type RandomIngressCondition struct {
	// Type of deployment condition.
	Type RandomIngressConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	LastHeartbeatTime metav1.Time `json:"lastHeartbeatTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// RandomIngressConditionType enumerates the possible conditions of a randomingress.
// +kubebuilder:validation:Enum=Valid;Progressing
type RandomIngressConditionType string

const (
	// Valid means the randomingress spec passes validation.
	RandomIngressValid RandomIngressConditionType = "Valid"

	// Progressing means the randomingress is currently changing the managed ingress.
	RandomIngressProgressing RandomIngressConditionType = "Progressing"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RandomIngress is the Schema for the randomingresses API
type RandomIngress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RandomIngressSpec   `json:"spec,omitempty"`
	Status RandomIngressStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RandomIngressList contains a list of RandomIngress
type RandomIngressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RandomIngress `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RandomIngress{}, &RandomIngressList{})
}
