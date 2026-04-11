package controller

import (
	"context"
	"fmt"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubesmithv1alpha1 "github.com/kubesmith/kubesmith-operator/api/v1alpha1"
)

// AppMonitorReconciler reconciles AppMonitor objects.
type AppMonitorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubesmith.io,resources=appmonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubesmith.io,resources=appmonitors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;patch;delete

func (r *AppMonitorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	monitor := &kubesmithv1alpha1.AppMonitor{}
	if err := r.Get(ctx, req.NamespacedName, monitor); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Owner references on child resources (ServiceMonitor, PrometheusRule) let
	// the K8s GC handle cleanup when the AppMonitor is deleted.
	if !monitor.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// ── ServiceMonitor ───────────────────────────────────────────────────────
	if monitor.Spec.Metrics != nil && monitor.Spec.Metrics.Enabled {
		if err := r.reconcileServiceMonitor(ctx, monitor); err != nil {
			logger.Error(err, "ServiceMonitor reconciliation failed")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("ServiceMonitor: %w", err)
		}
	}

	// ── PrometheusRule ───────────────────────────────────────────────────────
	if len(monitor.Spec.Alerts) > 0 {
		if err := r.reconcilePrometheusRule(ctx, monitor); err != nil {
			logger.Error(err, "PrometheusRule reconciliation failed")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("PrometheusRule: %w", err)
		}
	}

	// ── Status ───────────────────────────────────────────────────────────────
	now := metav1.Now()
	monitor.Status.LastReconciledAt = &now
	monitor.Status.Health = "Healthy"
	if err := r.Status().Update(ctx, monitor); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// reconcileServiceMonitor creates or updates the Prometheus ServiceMonitor that
// tells Prometheus how to scrape the referenced AppDeployment's pods.
func (r *AppMonitorReconciler) reconcileServiceMonitor(ctx context.Context, monitor *kubesmithv1alpha1.AppMonitor) error {
	m := monitor.Spec.Metrics
	port := m.Port
	if port == "" {
		port = "http"
	}
	path := m.Path
	if path == "" {
		path = "/metrics"
	}
	interval := monitoringv1.Duration(m.Interval)
	if interval == "" {
		interval = "30s"
	}

	desired := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      monitor.Name,
			Namespace: monitor.Namespace,
			// "release: monitoring" is the default serviceMonitorSelector label
			// used by kube-prometheus-stack to discover ServiceMonitors.
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kubesmith-operator",
				"release":                       "monitoring",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			// Select Services whose app.kubernetes.io/instance matches the
			// referenced AppDeployment (standard Helm label).
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/instance": monitor.Spec.AppDeploymentRef,
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{monitor.Namespace},
			},
			Endpoints: []monitoringv1.Endpoint{
				{Port: port, Path: path, Interval: interval},
			},
		},
	}

	if err := controllerutil.SetControllerReference(monitor, desired, r.Scheme); err != nil {
		return err
	}

	existing := &monitoringv1.ServiceMonitor{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create ServiceMonitor: %w", err)
		}
		monitor.Status.ServiceMonitorCreated = true
		return nil
	}
	if err != nil {
		return err
	}
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("update ServiceMonitor: %w", err)
	}
	monitor.Status.ServiceMonitorCreated = true
	return nil
}

// reconcilePrometheusRule creates or updates the PrometheusRule containing the
// alerting rules defined in the AppMonitor spec.
func (r *AppMonitorReconciler) reconcilePrometheusRule(ctx context.Context, monitor *kubesmithv1alpha1.AppMonitor) error {
	rules := make([]monitoringv1.Rule, 0, len(monitor.Spec.Alerts))
	for _, alert := range monitor.Spec.Alerts {
		severity := alert.Severity
		if severity == "" {
			severity = "warning"
		}
		summary := alert.Summary
		if summary == "" {
			summary = alert.Name
		}

		rule := monitoringv1.Rule{
			Alert: alert.Name,
			Expr:  intstr.FromString(alert.Expr),
			Labels: map[string]string{
				"severity":        severity,
				"app_deployment":  monitor.Spec.AppDeploymentRef,
			},
			Annotations: map[string]string{
				"summary": summary,
			},
		}
		if alert.For != "" {
			d := monitoringv1.Duration(alert.For)
			rule.For = &d
		}
		rules = append(rules, rule)
	}

	desired := &monitoringv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      monitor.Name + "-rules",
			Namespace: monitor.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kubesmith-operator",
				"release":                       "monitoring",
			},
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{Name: monitor.Name, Rules: rules},
			},
		},
	}

	if err := controllerutil.SetControllerReference(monitor, desired, r.Scheme); err != nil {
		return err
	}

	existing := &monitoringv1.PrometheusRule{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("create PrometheusRule: %w", err)
		}
		monitor.Status.PrometheusRuleCreated = true
		return nil
	}
	if err != nil {
		return err
	}
	existing.Spec = desired.Spec
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("update PrometheusRule: %w", err)
	}
	monitor.Status.PrometheusRuleCreated = true
	return nil
}

// SetupWithManager wires the reconciler into the controller-manager and
// declares ownership of the child resources it manages.
func (r *AppMonitorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubesmithv1alpha1.AppMonitor{}).
		Owns(&monitoringv1.ServiceMonitor{}).
		Owns(&monitoringv1.PrometheusRule{}).
		Complete(r)
}
