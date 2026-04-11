package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppMonitorSpec defines the desired monitoring configuration for an AppDeployment.
type AppMonitorSpec struct {
	// AppDeploymentRef is the name of the AppDeployment to monitor.
	// Must be in the same namespace as this AppMonitor.
	AppDeploymentRef string `json:"appDeploymentRef"`

	// Metrics configures Prometheus scraping via a ServiceMonitor.
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`

	// Alerts defines PromQL-based alerting rules to create as a PrometheusRule.
	// +optional
	Alerts []AlertRule `json:"alerts,omitempty"`
}

// MetricsSpec configures how Prometheus scrapes the application.
type MetricsSpec struct {
	// Enabled controls whether metrics scraping is active.
	Enabled bool `json:"enabled"`

	// Port is the service port name or number to scrape (e.g. "metrics" or "9090").
	// Defaults to "http" if not set.
	// +optional
	Port string `json:"port,omitempty"`

	// Path is the HTTP path that exposes metrics.
	// +kubebuilder:default=/metrics
	// +optional
	Path string `json:"path,omitempty"`

	// Interval is the Prometheus scrape interval (e.g. "30s", "1m").
	// +kubebuilder:default="30s"
	// +optional
	Interval string `json:"interval,omitempty"`
}

// AlertRule defines a single Prometheus alerting rule.
type AlertRule struct {
	// Name is the alert name (must be unique within the AppMonitor).
	Name string `json:"name"`

	// Expr is the PromQL expression. The alert fires when this evaluates to true.
	Expr string `json:"expr"`

	// For is how long the condition must hold before the alert fires (e.g. "5m").
	// +optional
	For string `json:"for,omitempty"`

	// Severity labels the alert for routing (info | warning | critical).
	// +kubebuilder:validation:Enum=info;warning;critical
	// +kubebuilder:default=warning
	// +optional
	Severity string `json:"severity,omitempty"`

	// Summary is a human-readable description of the alert.
	// +optional
	Summary string `json:"summary,omitempty"`
}

// AppMonitorStatus defines the observed state of AppMonitor.
type AppMonitorStatus struct {
	// Health is the overall monitoring health (Healthy | Degraded | Unknown).
	// +optional
	Health string `json:"health,omitempty"`

	// ServiceMonitorCreated is true when the Prometheus ServiceMonitor exists.
	ServiceMonitorCreated bool `json:"serviceMonitorCreated"`

	// PrometheusRuleCreated is true when the PrometheusRule exists.
	PrometheusRuleCreated bool `json:"prometheusRuleCreated"`

	// LastReconciledAt is the last time this AppMonitor was reconciled.
	// +optional
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`

	// Conditions is the set of status conditions.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AppDeployment",type="string",JSONPath=".spec.appDeploymentRef"
// +kubebuilder:printcolumn:name="Health",type="string",JSONPath=".status.health"
// +kubebuilder:printcolumn:name="ServiceMonitor",type="boolean",JSONPath=".status.serviceMonitorCreated"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AppMonitor is the Schema for the appmonitors API.
type AppMonitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppMonitorSpec   `json:"spec,omitempty"`
	Status AppMonitorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppMonitorList contains a list of AppMonitor.
type AppMonitorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppMonitor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppMonitor{}, &AppMonitorList{})
}
