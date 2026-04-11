package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Deployment lifecycle phases.
const (
	AppDeploymentFinalizer = "appdeployment.kubesmith.io/finalizer"

	PhaseInstalling = "Installing"
	PhaseRunning    = "Running"
	PhaseDegraded   = "Degraded"
	PhaseFailed     = "Failed"
	PhaseRemoving   = "Removing"
)

// AppDeploymentSpec defines the desired state of AppDeployment.
type AppDeploymentSpec struct {
	// Type is the deployment mechanism: "helm" or "manifest".
	// +kubebuilder:validation:Enum=helm;manifest
	Type string `json:"type"`

	// Helm contains the Helm chart configuration. Required when Type is "helm".
	// +optional
	Helm *HelmSpec `json:"helm,omitempty"`

	// Manifest is the raw Kubernetes YAML to apply. Required when Type is "manifest".
	// +optional
	Manifest string `json:"manifest,omitempty"`

	// PodSelector is a label selector used to find pods for manifest-type deployments.
	// Helm deployments derive the selector automatically from the release name.
	// +optional
	PodSelector map[string]string `json:"podSelector,omitempty"`
}

// HelmSpec configures a Helm chart deployment.
type HelmSpec struct {
	// RepoURL is the Helm repository URL (e.g. https://charts.bitnami.com/bitnami).
	RepoURL string `json:"repoUrl"`

	// Chart is the chart name within the repository.
	Chart string `json:"chart"`

	// Version pins the chart version. If empty the latest version is used.
	// +optional
	Version string `json:"version,omitempty"`

	// Values is additional YAML that is deep-merged with the chart's default values.
	// +optional
	Values string `json:"values,omitempty"`

	// ReleaseName overrides the Helm release name (defaults to the CR name).
	// +optional
	ReleaseName string `json:"releaseName,omitempty"`
}

// AppDeploymentStatus defines the observed state of AppDeployment.
type AppDeploymentStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message is a human-readable description of the current state.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions is the set of status conditions for this deployment.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ReadyPods is the count of pods that are Ready.
	ReadyPods int32 `json:"readyPods"`

	// TotalPods is the total number of pods belonging to this deployment.
	TotalPods int32 `json:"totalPods"`

	// DeployedVersion is the resolved chart version for helm-type deployments.
	// +optional
	DeployedVersion string `json:"deployedVersion,omitempty"`

	// LastDeployedAt is the timestamp of the last successful reconciliation.
	// +optional
	LastDeployedAt *metav1.Time `json:"lastDeployedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyPods"
// +kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.totalPods"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AppDeployment is the Schema for the appdeployments API.
type AppDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppDeploymentSpec   `json:"spec,omitempty"`
	Status AppDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppDeploymentList contains a list of AppDeployment.
type AppDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppDeployment{}, &AppDeploymentList{})
}
