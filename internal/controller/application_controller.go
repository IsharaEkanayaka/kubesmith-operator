package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/kubesmith/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ApplicationReconciler reconciles Application objects.
type ApplicationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=platform.kubesmith.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.kubesmith.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces;resourcequotas;limitranges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

const (
	applicationFinalizer = "applications.platform.kubesmith.io/finalizer"
	argocdServer         = "https://kubernetes.default.svc"

	// Condition types
	ConditionNamespaceReady  = "NamespaceReady"
	ConditionArgoCDReady     = "ArgoCDReady"
	ConditionMonitoringReady = "MonitoringReady"
	ConditionRBACReady       = "RBACReady"
	ConditionNetworkReady    = "NetworkReady"
)

func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	app := &platformv1alpha1.Application{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !app.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, app)
	}

	if !controllerutil.ContainsFinalizer(app, applicationFinalizer) {
		controllerutil.AddFinalizer(app, applicationFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Phase 1: Namespace management
	if err := r.reconcileNamespace(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile namespace")
		r.setCondition(app, ConditionNamespaceReady, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}
	r.setCondition(app, ConditionNamespaceReady, metav1.ConditionTrue, "Reconciled", "Namespace ready")

	if err := r.reconcileResourceQuota(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile ResourceQuota")
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}

	if err := r.reconcileLimitRange(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile LimitRange")
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}

	// Phase 2: Argo CD Application
	if err := r.reconcileArgoApplication(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile Argo CD Application")
		r.setCondition(app, ConditionArgoCDReady, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}
	r.setCondition(app, ConditionArgoCDReady, metav1.ConditionTrue, "Reconciled", "Argo CD Application ready")

	// Read back Argo CD status
	r.readArgoStatus(ctx, app)

	// Phase 2: ServiceMonitor
	if err := r.reconcileServiceMonitor(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile ServiceMonitor")
		r.setCondition(app, ConditionMonitoringReady, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}
	if app.Spec.Monitoring != nil && app.Spec.Monitoring.Metrics != nil && app.Spec.Monitoring.Metrics.Enabled {
		r.setCondition(app, ConditionMonitoringReady, metav1.ConditionTrue, "Reconciled", "ServiceMonitor active")
	} else {
		r.setCondition(app, ConditionMonitoringReady, metav1.ConditionFalse, "Disabled", "Monitoring not enabled")
	}

	// Phase 3: RBAC
	if err := r.reconcileRBAC(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile RBAC")
		r.setCondition(app, ConditionRBACReady, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}
	if app.Spec.RBAC != nil {
		r.setCondition(app, ConditionRBACReady, metav1.ConditionTrue, "Reconciled", "RBAC configured")
	} else {
		r.setCondition(app, ConditionRBACReady, metav1.ConditionFalse, "Disabled", "RBAC not configured")
	}

	// Phase 3: NetworkPolicy
	if err := r.reconcileNetworkPolicy(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile NetworkPolicy")
		r.setCondition(app, ConditionNetworkReady, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}
	if app.Spec.Network != nil {
		r.setCondition(app, ConditionNetworkReady, metav1.ConditionTrue, "Reconciled", "Network policies active")
	} else {
		r.setCondition(app, ConditionNetworkReady, metav1.ConditionFalse, "Disabled", "Network policies not configured")
	}

	return r.updateStatus(ctx, app, "Ready", "Reconciled")
}

func (r *ApplicationReconciler) handleDeletion(ctx context.Context, app *platformv1alpha1.Application) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(app, applicationFinalizer) {
		r.Recorder.Event(app, corev1.EventTypeNormal, "Deleting", "Cleaning up child resources")
		_ = r.deleteArgoApplication(ctx, app)
		_ = r.deleteServiceMonitor(ctx, app)
		_ = r.deleteRBAC(ctx, app)
		_ = r.deleteNetworkPolicies(ctx, app)
		controllerutil.RemoveFinalizer(app, applicationFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(app, corev1.EventTypeNormal, "Deleted", "All child resources cleaned up")
	}
	return ctrl.Result{}, nil
}

func (r *ApplicationReconciler) reconcileArgoApplication(ctx context.Context, app *platformv1alpha1.Application) error {
	argocdNamespace := getEnv("ARGOCD_NAMESPACE", "argocd")
	project := getEnv("ARGOCD_PROJECT", "default")

	deploy := app.Spec.Deploy
	syncPolicy := "manual"
	prune := false
	selfHeal := false
	if deploy != nil {
		if deploy.SyncPolicy != "" {
			syncPolicy = deploy.SyncPolicy
		}
		prune = deploy.Prune
		selfHeal = deploy.SelfHeal
	}

	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(mustGVK("argoproj.io", "v1alpha1", "Application"))
	desired.SetName(app.Name)
	desired.SetNamespace(argocdNamespace)
	desired.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "kubesmith-operator",
		"app.kubernetes.io/part-of":    app.Name,
	})

	spec := map[string]interface{}{
		"project": project,
		"source": map[string]interface{}{
			"repoURL":        app.Spec.Source.RepoURL,
			"path":           app.Spec.Source.Path,
			"targetRevision": app.Spec.Source.Revision,
		},
		"destination": map[string]interface{}{
			"server":    argocdServer,
			"namespace": app.Spec.Destination.Namespace,
		},
	}
	if syncPolicy == "auto" {
		spec["syncPolicy"] = map[string]interface{}{
			"automated": map[string]interface{}{
				"prune":    prune,
				"selfHeal": selfHeal,
			},
		}
	}

	if err := unstructured.SetNestedMap(desired.Object, spec, "spec"); err != nil {
		return err
	}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, desired); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "ArgoCDCreated", "Created Argo CD Application %s", app.Name)
			return nil
		}
		if apimeta.IsNoMatchError(err) {
			return nil // Argo CD CRD not installed, skip
		}
		return err
	}

	current.Object["spec"] = spec
	current.SetLabels(desired.GetLabels())
	if err := r.Update(ctx, current); err != nil {
		return err
	}
	return nil
}

// readArgoStatus reads the Argo CD Application status and populates ArgoStatus.
func (r *ApplicationReconciler) readArgoStatus(ctx context.Context, app *platformv1alpha1.Application) {
	argocdNamespace := getEnv("ARGOCD_NAMESPACE", "argocd")

	argoApp := &unstructured.Unstructured{}
	argoApp.SetGroupVersionKind(mustGVK("argoproj.io", "v1alpha1", "Application"))
	if err := r.Get(ctx, client.ObjectKey{Name: app.Name, Namespace: argocdNamespace}, argoApp); err != nil {
		return // silently skip if we can't read it
	}

	argoStatus := &platformv1alpha1.ArgoStatus{}

	if syncStatus, found, err := unstructured.NestedString(argoApp.Object, "status", "sync", "status"); err == nil && found {
		argoStatus.SyncStatus = syncStatus
	}
	if healthStatus, found, err := unstructured.NestedString(argoApp.Object, "status", "health", "status"); err == nil && found {
		argoStatus.HealthStatus = healthStatus
	}

	app.Status.ArgoStatus = argoStatus
}

func (r *ApplicationReconciler) reconcileServiceMonitor(ctx context.Context, app *platformv1alpha1.Application) error {
	metrics := app.Spec.Monitoring
	if metrics == nil || metrics.Metrics == nil || !metrics.Metrics.Enabled {
		return r.deleteServiceMonitor(ctx, app)
	}

	releaseLabel := getEnv("PROMETHEUS_RELEASE", "monitoring")
	port := metrics.Metrics.Port
	if port == "" {
		port = "http"
	}
	path := metrics.Metrics.Path
	if path == "" {
		path = "/metrics"
	}

	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(mustGVK("monitoring.coreos.com", "v1", "ServiceMonitor"))
	desired.SetName(app.Name)
	desired.SetNamespace(app.Spec.Destination.Namespace)
	desired.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "kubesmith-operator",
		"release":                      releaseLabel,
	})

	spec := map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app.kubernetes.io/instance": app.Name,
			},
		},
		"endpoints": []interface{}{
			map[string]interface{}{
				"port": port,
				"path": path,
			},
		},
	}

	if err := unstructured.SetNestedMap(desired.Object, spec, "spec"); err != nil {
		return err
	}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), current); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, desired); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "ServiceMonitorCreated", "Created ServiceMonitor %s", app.Name)
			return nil
		}
		if apimeta.IsNoMatchError(err) {
			return nil // ServiceMonitor CRD not installed, skip
		}
		return err
	}

	current.Object["spec"] = spec
	current.SetLabels(desired.GetLabels())
	return r.Update(ctx, current)
}

func (r *ApplicationReconciler) deleteArgoApplication(ctx context.Context, app *platformv1alpha1.Application) error {
	argocdNamespace := getEnv("ARGOCD_NAMESPACE", "argocd")
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(mustGVK("argoproj.io", "v1alpha1", "Application"))
	obj.SetName(app.Name)
	obj.SetNamespace(argocdNamespace)
	if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) && !apimeta.IsNoMatchError(err) {
		return err
	}
	return nil
}

func (r *ApplicationReconciler) deleteServiceMonitor(ctx context.Context, app *platformv1alpha1.Application) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(mustGVK("monitoring.coreos.com", "v1", "ServiceMonitor"))
	obj.SetName(app.Name)
	obj.SetNamespace(app.Spec.Destination.Namespace)
	if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) && !apimeta.IsNoMatchError(err) {
		return err
	}
	return nil
}

func (r *ApplicationReconciler) setCondition(app *platformv1alpha1.Application, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range app.Status.Conditions {
		if c.Type == condType {
			if c.Status != status || c.Reason != reason || c.Message != message {
				app.Status.Conditions[i].Status = status
				app.Status.Conditions[i].Reason = reason
				app.Status.Conditions[i].Message = message
				app.Status.Conditions[i].LastTransitionTime = now
				app.Status.Conditions[i].ObservedGeneration = app.Generation
			}
			return
		}
	}
	app.Status.Conditions = append(app.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		ObservedGeneration: app.Generation,
	})
}

func (r *ApplicationReconciler) updateStatus(ctx context.Context, app *platformv1alpha1.Application, phase, message string) (ctrl.Result, error) {
	now := metav1.Now()
	app.Status.Phase = phase
	app.Status.Message = message
	app.Status.LastSyncedAt = &now
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}
	if phase == "Degraded" {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func mustGVK(group, version, kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

// ensure fmt is used
var _ = fmt.Sprintf

// SetupWithManager wires the reconciler into the controller-manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Application{}).
		Complete(r)
}
