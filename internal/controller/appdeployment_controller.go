package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubesmithv1alpha1 "github.com/kubesmith/kubesmith-operator/api/v1alpha1"
	"github.com/kubesmith/kubesmith-operator/internal/helm"
)

// AppDeploymentReconciler reconciles AppDeployment objects.
type AppDeploymentReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Config     *rest.Config
	RESTMapper meta.RESTMapper
	HelmClient *helm.Client
}

// +kubebuilder:rbac:groups=kubesmith.io,resources=appdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubesmith.io,resources=appdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubesmith.io,resources=appdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch;create;update;patch;delete

func (r *AppDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	app := &kubesmithv1alpha1.AppDeployment{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// ── Deletion ─────────────────────────────────────────────────────────────
	if !app.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, app)
	}

	// ── Finalizer ────────────────────────────────────────────────────────────
	if !controllerutil.ContainsFinalizer(app, kubesmithv1alpha1.AppDeploymentFinalizer) {
		controllerutil.AddFinalizer(app, kubesmithv1alpha1.AppDeploymentFinalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// ── First-time status ─────────────────────────────────────────────────────
	if app.Status.Phase == "" {
		app.Status.Phase = kubesmithv1alpha1.PhaseInstalling
		app.Status.Message = "Starting deployment"
		if err := r.Status().Update(ctx, app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ── Deploy ────────────────────────────────────────────────────────────────
	if err := r.reconcileDeployment(ctx, app); err != nil {
		logger.Error(err, "deployment failed", "name", app.Name)
		app.Status.Phase = kubesmithv1alpha1.PhaseFailed
		app.Status.Message = err.Error()
		_ = r.Status().Update(ctx, app)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// ── Pod status ────────────────────────────────────────────────────────────
	if err := r.updatePodStatus(ctx, app); err != nil {
		logger.Error(err, "pod status update failed")
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// handleDeletion runs cleanup and removes the finalizer.
func (r *AppDeploymentReconciler) handleDeletion(ctx context.Context, app *kubesmithv1alpha1.AppDeployment) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(app, kubesmithv1alpha1.AppDeploymentFinalizer) {
		return ctrl.Result{}, nil
	}

	app.Status.Phase = kubesmithv1alpha1.PhaseRemoving
	app.Status.Message = "Removing deployment"
	_ = r.Status().Update(ctx, app)

	if err := r.cleanup(ctx, app); err != nil {
		return ctrl.Result{RequeueAfter: 15 * time.Second}, err
	}

	controllerutil.RemoveFinalizer(app, kubesmithv1alpha1.AppDeploymentFinalizer)
	return ctrl.Result{}, r.Update(ctx, app)
}

func (r *AppDeploymentReconciler) reconcileDeployment(ctx context.Context, app *kubesmithv1alpha1.AppDeployment) error {
	switch app.Spec.Type {
	case "helm":
		if app.Spec.Helm == nil {
			return fmt.Errorf("spec.helm is required when type=helm")
		}
		return r.HelmClient.InstallOrUpgrade(ctx, app.Namespace, app.Spec.Helm, app.Name)

	case "manifest":
		if app.Spec.Manifest == "" {
			return fmt.Errorf("spec.manifest is required when type=manifest")
		}
		return r.applyManifest(ctx, app.Namespace, app.Spec.Manifest)

	default:
		return fmt.Errorf("unknown deployment type %q (must be helm or manifest)", app.Spec.Type)
	}
}

func (r *AppDeploymentReconciler) cleanup(ctx context.Context, app *kubesmithv1alpha1.AppDeployment) error {
	switch app.Spec.Type {
	case "helm":
		if app.Spec.Helm == nil {
			return nil
		}
		releaseName := app.Name
		if app.Spec.Helm.ReleaseName != "" {
			releaseName = app.Spec.Helm.ReleaseName
		}
		return r.HelmClient.Uninstall(ctx, app.Namespace, releaseName)

	case "manifest":
		if app.Spec.Manifest == "" {
			return nil
		}
		return r.deleteManifest(ctx, app.Namespace, app.Spec.Manifest)
	}
	return nil
}

// applyManifest uses server-side apply to create or update every K8s object
// found in the YAML string.
func (r *AppDeploymentReconciler) applyManifest(ctx context.Context, ns, manifest string) error {
	dynClient, err := dynamic.NewForConfig(r.Config)
	if err != nil {
		return fmt.Errorf("dynamic client: %w", err)
	}

	decoder := k8syaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		rawExt := &runtime.RawExtension{}
		if err := decoder.Decode(rawExt); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode manifest: %w", err)
		}
		if rawExt.Raw == nil {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(rawExt.Raw, obj); err != nil {
			return fmt.Errorf("unmarshal object: %w", err)
		}
		if obj.GetKind() == "" {
			continue
		}

		gvk := obj.GroupVersionKind()
		mapping, err := r.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("REST mapping for %s: %w", gvk.Kind, err)
		}

		if obj.GetNamespace() == "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			obj.SetNamespace(ns)
		}

		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			dr = dynClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
		} else {
			dr = dynClient.Resource(mapping.Resource)
		}

		data, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		force := true
		if _, err := dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data,
			metav1.PatchOptions{FieldManager: "kubesmith-operator", Force: &force}); err != nil {
			return fmt.Errorf("apply %s %q: %w", gvk.Kind, obj.GetName(), err)
		}
	}
	return nil
}

// deleteManifest deletes every K8s object found in the YAML string.
func (r *AppDeploymentReconciler) deleteManifest(ctx context.Context, ns, manifest string) error {
	dynClient, err := dynamic.NewForConfig(r.Config)
	if err != nil {
		return fmt.Errorf("dynamic client: %w", err)
	}

	decoder := k8syaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		rawExt := &runtime.RawExtension{}
		if err := decoder.Decode(rawExt); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode manifest: %w", err)
		}
		if rawExt.Raw == nil {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(rawExt.Raw, obj); err != nil {
			continue
		}
		if obj.GetKind() == "" {
			continue
		}

		gvk := obj.GroupVersionKind()
		mapping, err := r.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			continue // resource type may no longer exist
		}

		objNs := obj.GetNamespace()
		if objNs == "" {
			objNs = ns
		}

		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			dr = dynClient.Resource(mapping.Resource).Namespace(objNs)
		} else {
			dr = dynClient.Resource(mapping.Resource)
		}

		if err := dr.Delete(ctx, obj.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete %s %q: %w", gvk.Kind, obj.GetName(), err)
		}
	}
	return nil
}

// updatePodStatus lists pods belonging to the deployment and refreshes Status.
func (r *AppDeploymentReconciler) updatePodStatus(ctx context.Context, app *kubesmithv1alpha1.AppDeployment) error {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{client.InNamespace(app.Namespace)}

	switch app.Spec.Type {
	case "helm":
		releaseName := app.Name
		if app.Spec.Helm != nil && app.Spec.Helm.ReleaseName != "" {
			releaseName = app.Spec.Helm.ReleaseName
		}
		listOpts = append(listOpts, client.MatchingLabels{"app.kubernetes.io/instance": releaseName})
	default:
		if len(app.Spec.PodSelector) > 0 {
			listOpts = append(listOpts, client.MatchingLabels(app.Spec.PodSelector))
		}
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		return err
	}

	var readyCount int32
	for _, pod := range podList.Items {
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				readyCount++
				break
			}
		}
	}

	total := int32(len(podList.Items))
	app.Status.TotalPods = total
	app.Status.ReadyPods = readyCount

	switch {
	case total == 0:
		app.Status.Phase = kubesmithv1alpha1.PhaseInstalling
		app.Status.Message = "Waiting for pods"
	case readyCount == total:
		app.Status.Phase = kubesmithv1alpha1.PhaseRunning
		app.Status.Message = fmt.Sprintf("%d/%d pods ready", readyCount, total)
		now := metav1.Now()
		app.Status.LastDeployedAt = &now
	case readyCount > 0:
		app.Status.Phase = kubesmithv1alpha1.PhaseDegraded
		app.Status.Message = fmt.Sprintf("%d/%d pods ready", readyCount, total)
	default:
		app.Status.Phase = kubesmithv1alpha1.PhaseInstalling
		app.Status.Message = fmt.Sprintf("0/%d pods ready", total)
	}

	return r.Status().Update(ctx, app)
}

// SetupWithManager wires the reconciler into the controller-manager.
func (r *AppDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.RESTMapper = mgr.GetRESTMapper()
	r.HelmClient = helm.NewClient(r.Config)
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubesmithv1alpha1.AppDeployment{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
