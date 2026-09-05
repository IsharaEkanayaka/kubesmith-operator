package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

	// RBAC configures namespace access for teams.
	// +optional
	RBAC *ApplicationRBAC `json:"rbac,omitempty"`

	// Network configures network isolation.
	// +optional
	Network *ApplicationNetwork `json:"network,omitempty"`
}

// ApplicationSource declares where the manifests live.
type ApplicationSource struct {
	RepoURL  string `json:"repoUrl"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// ApplicationDestination declares the target namespace and its resource limits.
type ApplicationDestination struct {
	Namespace string `json:"namespace"`

	// ResourceQuota defines resource limits for the namespace.
	// +optional
	ResourceQuota *ResourceQuotaSpec `json:"resourceQuota,omitempty"`

	// LimitRange defines default container limits.
	// +optional
	LimitRange *LimitRangeSpec `json:"limitRange,omitempty"`
}

// ResourceQuotaSpec configures namespace-level resource limits.
type ResourceQuotaSpec struct {
	// CPU is the total CPU limit (e.g. "4").
	// +optional
	CPU string `json:"cpu,omitempty"`
	// Memory is the total memory limit (e.g. "8Gi").
	// +optional
	Memory string `json:"memory,omitempty"`
	// Pods is the max number of pods.
	// +optional
	Pods string `json:"pods,omitempty"`
}

// LimitRangeSpec configures default container resource limits.
type LimitRangeSpec struct {
	// DefaultCPU is the default CPU limit per container (e.g. "500m").
	// +optional
	DefaultCPU string `json:"defaultCpu,omitempty"`
	// DefaultMemory is the default memory limit per container (e.g. "512Mi").
	// +optional
	DefaultMemory string `json:"defaultMemory,omitempty"`
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

// ApplicationRBAC configures namespace access for teams.
type ApplicationRBAC struct {
	// Owners are users/groups with admin access to the namespace.
	// +optional
	Owners []string `json:"owners,omitempty"`
	// Viewers are users/groups with read-only access.
	// +optional
	Viewers []string `json:"viewers,omitempty"`
}

// ApplicationNetwork configures network isolation.
type ApplicationNetwork struct {
	// AllowFromNamespaces lists namespaces allowed to send traffic.
	// +optional
	AllowFromNamespaces []string `json:"allowFromNamespaces,omitempty"`
	// DenyAll if true, blocks all ingress except from allowed namespaces.
	// +optional
	DenyAll bool `json:"denyAll,omitempty"`
}

// ApplicationStatus defines the observed state of Application.
type ApplicationStatus struct {
	// Phase is the high-level state: Ready, Degraded, Progressing.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message is a human-readable description.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncedAt is the last time reconciliation succeeded.
	// +optional
	LastSyncedAt *metav1.Time `json:"lastSyncedAt,omitempty"`

	// ArgoStatus reflects the Argo CD Application status.
	// +optional
	ArgoStatus *ArgoStatus `json:"argoStatus,omitempty"`
}

// ArgoStatus holds status information pulled from the Argo CD Application.
type ArgoStatus struct {
	// SyncStatus is the Argo CD sync status (Synced, OutOfSync, Unknown).
	// +optional
	SyncStatus string `json:"syncStatus,omitempty"`
	// HealthStatus is the Argo CD health status (Healthy, Degraded, Progressing, etc).
	// +optional
	HealthStatus string `json:"healthStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Sync",type=string,JSONPath=`.status.argoStatus.syncStatus`
// +kubebuilder:printcolumn:name="Health",type=string,JSONPath=`.status.argoStatus.healthStatus`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

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

// DeepCopyObject implements runtime.Object for Application.
func (in *Application) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(Application)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties into another Application.
func (in *Application) DeepCopyInto(out *Application) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()

	// Spec.Destination
	if in.Spec.Destination.ResourceQuota != nil {
		rq := *in.Spec.Destination.ResourceQuota
		out.Spec.Destination.ResourceQuota = &rq
	}
	if in.Spec.Destination.LimitRange != nil {
		lr := *in.Spec.Destination.LimitRange
		out.Spec.Destination.LimitRange = &lr
	}

	// Spec.Deploy
	if in.Spec.Deploy != nil {
		deploy := *in.Spec.Deploy
		out.Spec.Deploy = &deploy
	}

	// Spec.Monitoring
	if in.Spec.Monitoring != nil {
		monitoring := *in.Spec.Monitoring
		if in.Spec.Monitoring.Metrics != nil {
			metrics := *in.Spec.Monitoring.Metrics
			monitoring.Metrics = &metrics
		}
		out.Spec.Monitoring = &monitoring
	}

	// Spec.RBAC
	if in.Spec.RBAC != nil {
		rbac := *in.Spec.RBAC
		if in.Spec.RBAC.Owners != nil {
			rbac.Owners = make([]string, len(in.Spec.RBAC.Owners))
			copy(rbac.Owners, in.Spec.RBAC.Owners)
		}
		if in.Spec.RBAC.Viewers != nil {
			rbac.Viewers = make([]string, len(in.Spec.RBAC.Viewers))
			copy(rbac.Viewers, in.Spec.RBAC.Viewers)
		}
		out.Spec.RBAC = &rbac
	}

	// Spec.Network
	if in.Spec.Network != nil {
		network := *in.Spec.Network
		if in.Spec.Network.AllowFromNamespaces != nil {
			network.AllowFromNamespaces = make([]string, len(in.Spec.Network.AllowFromNamespaces))
			copy(network.AllowFromNamespaces, in.Spec.Network.AllowFromNamespaces)
		}
		out.Spec.Network = &network
	}

	// Status.Conditions
	if in.Status.Conditions != nil {
		out.Status.Conditions = make([]metav1.Condition, len(in.Status.Conditions))
		for i := range in.Status.Conditions {
			in.Status.Conditions[i].DeepCopyInto(&out.Status.Conditions[i])
		}
	}

	// Status.LastSyncedAt
	if in.Status.LastSyncedAt != nil {
		last := *in.Status.LastSyncedAt
		out.Status.LastSyncedAt = &last
	}

	// Status.ArgoStatus
	if in.Status.ArgoStatus != nil {
		argoStatus := *in.Status.ArgoStatus
		out.Status.ArgoStatus = &argoStatus
	}
}

// DeepCopyObject implements runtime.Object for ApplicationList.
func (in *ApplicationList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(ApplicationList)
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		out.Items = make([]Application, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
	return out
}
