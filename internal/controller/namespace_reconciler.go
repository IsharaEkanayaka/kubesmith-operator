package controller

import (
	"context"

	"github.com/kubesmith/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *ApplicationReconciler) reconcileNamespace(ctx context.Context, app *v1alpha1.Application) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: app.Spec.Destination.Namespace}, ns)
	if err != nil {
		if apierrors.IsNotFound(err) {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: app.Spec.Destination.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "kubesmith-operator",
					},
				},
			}
			if err := r.Create(ctx, ns); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "NamespaceCreated", "Created namespace %s", app.Spec.Destination.Namespace)
			return nil
		}
		return err
	}
	return nil
}

func (r *ApplicationReconciler) reconcileResourceQuota(ctx context.Context, app *v1alpha1.Application) error {
	quotaName := "kubesmith-quota"
	ns := app.Spec.Destination.Namespace

	if app.Spec.Destination.ResourceQuota == nil {
		quota := &corev1.ResourceQuota{}
		err := r.Get(ctx, types.NamespacedName{Name: quotaName, Namespace: ns}, quota)
		if err == nil {
			if err := r.Delete(ctx, quota); err != nil {
				return err
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}

	spec := app.Spec.Destination.ResourceQuota
	quota := &corev1.ResourceQuota{}
	err := r.Get(ctx, types.NamespacedName{Name: quotaName, Namespace: ns}, quota)

	rl := corev1.ResourceList{}
	if spec.CPU != "" {
		rl[corev1.ResourceLimitsCPU] = resource.MustParse(spec.CPU)
	}
	if spec.Memory != "" {
		rl[corev1.ResourceLimitsMemory] = resource.MustParse(spec.Memory)
	}
	if spec.Pods != "" {
		rl[corev1.ResourcePods] = resource.MustParse(spec.Pods)
	}

	desiredQuota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      quotaName,
			Namespace: ns,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: rl,
		},
	}

	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, desiredQuota); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "ResourceQuotaCreated", "Created ResourceQuota %s in namespace %s", quotaName, ns)
			return nil
		}
		return err
	}

	quota.Spec.Hard = rl
	if err := r.Update(ctx, quota); err != nil {
		return err
	}
	return nil
}

func (r *ApplicationReconciler) reconcileLimitRange(ctx context.Context, app *v1alpha1.Application) error {
	limitRangeName := "kubesmith-defaults"
	ns := app.Spec.Destination.Namespace

	if app.Spec.Destination.LimitRange == nil {
		lr := &corev1.LimitRange{}
		err := r.Get(ctx, types.NamespacedName{Name: limitRangeName, Namespace: ns}, lr)
		if err == nil {
			if err := r.Delete(ctx, lr); err != nil {
				return err
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}

	spec := app.Spec.Destination.LimitRange
	lr := &corev1.LimitRange{}
	err := r.Get(ctx, types.NamespacedName{Name: limitRangeName, Namespace: ns}, lr)

	defaultLimits := corev1.ResourceList{}
	if spec.DefaultCPU != "" {
		defaultLimits[corev1.ResourceCPU] = resource.MustParse(spec.DefaultCPU)
	}
	if spec.DefaultMemory != "" {
		defaultLimits[corev1.ResourceMemory] = resource.MustParse(spec.DefaultMemory)
	}

	limits := []corev1.LimitRangeItem{}
	if len(defaultLimits) > 0 {
		limits = append(limits, corev1.LimitRangeItem{
			Type:    corev1.LimitTypeContainer,
			Default: defaultLimits,
		})
	}

	desiredLR := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      limitRangeName,
			Namespace: ns,
		},
		Spec: corev1.LimitRangeSpec{
			Limits: limits,
		},
	}

	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, desiredLR); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "LimitRangeCreated", "Created LimitRange %s in namespace %s", limitRangeName, ns)
			return nil
		}
		return err
	}

	lr.Spec.Limits = limits
	if err := r.Update(ctx, lr); err != nil {
		return err
	}
	return nil
}
