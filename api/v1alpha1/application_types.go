package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ApplicationSpec defines the desired state of Application.
type ApplicationSpec struct {
	// Source describes the Git repository and path.
	Source ApplicationSource `json:"source"`

	// Destination is the target namespace for deployment.
	Destination ApplicationDestination `json:"destination"`

	// Deploy controls GitOps sync behavior.
	// +optional
	Deploy *ApplicationDeploy `json:"deploy,omitempty"`

	// Monitoring configures metrics scraping.
	// +optional
	Monitoring *ApplicationMonitoring `json:"monitoring,omitempty"`
}

// ApplicationSource declares where the manifests live.
type ApplicationSource struct {
	RepoURL  string `json:"repoUrl"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// ApplicationDestination declares the target namespace.
type ApplicationDestination struct {
	Namespace string `json:"namespace"`
}

// ApplicationDeploy controls GitOps sync behavior.
type ApplicationDeploy struct {
	// SyncPolicy is "auto" or "manual".
	// +kubebuilder:validation:Enum=auto;manual
	SyncPolicy string `json:"syncPolicy"`

	// Prune enables resource pruning.
	// +optional
	Prune bool `json:"prune,omitempty"`

	// SelfHeal enables self-healing.
	// +optional
	SelfHeal bool `json:"selfHeal,omitempty"`
}

// ApplicationMonitoring declares metrics scraping settings.
type ApplicationMonitoring struct {
	// Metrics enables Prometheus metrics scraping.
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`
}

// MetricsSpec configures the ServiceMonitor endpoint.
type MetricsSpec struct {
	// Enabled controls whether metrics scraping is active.
	Enabled bool `json:"enabled"`

	// Port is the service port name or number.
	// +optional
	Port string `json:"port,omitempty"`

	// Path is the metrics path.
	// +kubebuilder:default=/metrics
	// +optional
	Path string `json:"path,omitempty"`
}

// ApplicationStatus defines the observed state of Application.
type ApplicationStatus struct {
	// Phase is the high-level state.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message is a human-readable description.
	// +optional
	Message string `json:"message,omitempty"`

	// LastSyncedAt is the last time reconciliation succeeded.
	// +optional
	LastSyncedAt *metav1.Time `json:"lastSyncedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Application is the Schema for the applications API.
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApplicationList contains a list of Application.
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
