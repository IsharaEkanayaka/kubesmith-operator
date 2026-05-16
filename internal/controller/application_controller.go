package controller

import (
	"context"
	"os"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/kubesmith/operator/api/v1alpha1"
)

// ApplicationReconciler reconciles Application objects.
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=platform.kubesmith.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.kubesmith.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=argoproj.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete

const (
	applicationFinalizer = "applications.platform.kubesmith.io/finalizer"
	argocdServer         = "https://kubernetes.default.svc"
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

	if err := r.reconcileArgoApplication(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile Argo CD Application")
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}

	if err := r.reconcileServiceMonitor(ctx, app); err != nil {
		logger.Error(err, "failed to reconcile ServiceMonitor")
		return r.updateStatus(ctx, app, "Degraded", err.Error())
	}

	return r.updateStatus(ctx, app, "Ready", "Reconciled")
}

func (r *ApplicationReconciler) handleDeletion(ctx context.Context, app *platformv1alpha1.Application) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(app, applicationFinalizer) {
		_ = r.deleteArgoApplication(ctx, app)
		_ = r.deleteServiceMonitor(ctx, app)
		controllerutil.RemoveFinalizer(app, applicationFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, err
		}
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
			return r.Create(ctx, desired)
		}
		return err
	}

	current.Object["spec"] = spec
	current.SetLabels(desired.GetLabels())
	return r.Update(ctx, current)
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
		"release":                       releaseLabel,
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
			return r.Create(ctx, desired)
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
	if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ApplicationReconciler) deleteServiceMonitor(ctx context.Context, app *platformv1alpha1.Application) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(mustGVK("monitoring.coreos.com", "v1", "ServiceMonitor"))
	obj.SetName(app.Name)
	obj.SetNamespace(app.Spec.Destination.Namespace)
	if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *ApplicationReconciler) updateStatus(ctx context.Context, app *platformv1alpha1.Application, phase, message string) (ctrl.Result, error) {
	now := metav1.Now()
	app.Status.Phase = phase
	app.Status.Message = message
	app.Status.LastSyncedAt = &now
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, err
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

// SetupWithManager wires the reconciler into the controller-manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Application{}).
		Complete(r)
}
