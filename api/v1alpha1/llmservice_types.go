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

type SecretKeyRef struct {
	Name string `json:"name"`

	Key string `json:"key,omitempty"`
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LLMServiceSpec defines the desired state of LLMService
type LLMServiceSpec struct {
	Model string `json:"model"`

	// +kubebuilder:default="vllm/vllm-openai:v0.30.0"
	Image string `json:"image,omitempty"`

	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	GPUs int32 `json:"gpus"`

	// +kubebuilder:default=64
	// +kubebuilder:validation:Minimum=1
	MaxNumSeqs int32 `json:"maxNumSeqs"`

	// +kubebuilder:default=4096
	// +kubebuilder:validation:Minimum=1
	MaxModelLen int32 `json:"maxModelLen"`

	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	TensorParallelSize int32 `json:"tensorParallelSize"`

	// +optional
	HFTokenSecretRef *SecretKeyRef `json:"hfTokenSecretRef,omitempty"`

	// +kubebuilder:default="20Gi"
	CacheSize string `json:"cacheSize"`

	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`
}

type AutoscalingSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	MinReplicas int32 `json:"minReplicas"`

	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	TargetWaitingRequests int32 `json:"targetWaitingRequests"`
}

type LLMServicePhase string

const (
	LLMServicePhasePending LLMServicePhase = "Pending"
	LLMServicePhaseReady   LLMServicePhase = "Ready"
	LLMServicePhaseFailed  LLMServicePhase = "Failed"
)

// LLMServiceStatus defines the observed state of LLMService.
type LLMServiceStatus struct {
	Phase LLMServicePhase `json:"phase,omitempty"`

	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	Endpoint string `json:"endpoint,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`

	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// LLMService is the Schema for the llmservices API
type LLMService struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LLMService
	// +required
	Spec LLMServiceSpec `json:"spec"`

	// status defines the observed state of LLMService
	// +optional
	Status LLMServiceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LLMServiceList contains a list of LLMService
type LLMServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LLMService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMService{}, &LLMServiceList{})
}
