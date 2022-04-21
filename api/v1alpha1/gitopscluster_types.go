/*
Copyright 2022.

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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fluxcd/pkg/apis/meta"
)

const defaultWaitDuration = time.Second * 60

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GitopsClusterSpec defines the desired state of GitopsCluster
type GitopsClusterSpec struct {
	// SecretRef specifies the Secret containing the kubeconfig for a cluster.
	// +optional
	SecretRef *meta.LocalObjectReference `json:"secretRef,omitempty"`
	// CAPIClusterRef specifies the CAPI Cluster.
	// +optional
	CAPIClusterRef *meta.LocalObjectReference `json:"capiClusterRef,omitempty"`
	// When checking for readiness, this is the time to wait before
	// checking again.
	//+kubebuilder:default:60s
	//+optional
	ClusterReadinessBackoff *metav1.Duration `json:"clusterReadinessBackoff,omitempty"`
}

// GitopsClusterStatus defines the observed state of GitopsCluster
type GitopsClusterStatus struct {
	// Conditions holds the conditions for the Cluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the status conditions of the object.
func (in GitopsCluster) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the object.
func (in *GitopsCluster) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""

// GitopsCluster is the Schema for the gitopsclusters API
type GitopsCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitopsClusterSpec   `json:"spec,omitempty"`
	Status GitopsClusterStatus `json:"status,omitempty"`
}

// ClusterReadinessRequeue returns the configured ClusterReadinessBackoff or a default
// value if not configured.
func (c GitopsCluster) ClusterReadinessRequeue() time.Duration {
	if v := c.Spec.ClusterReadinessBackoff; v != nil {
		return v.Duration
	}
	return defaultWaitDuration
}

// +kubebuilder:object:root=true

// GitopsClusterList contains a list of GitopsCluster
type GitopsClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitopsCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitopsCluster{}, &GitopsClusterList{})
}
