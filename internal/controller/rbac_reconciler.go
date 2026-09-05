package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/kubesmith/operator/api/v1alpha1"
)

func (r *ApplicationReconciler) reconcileRBAC(ctx context.Context, app *platformv1alpha1.Application) error {
	if app.Spec.RBAC == nil {
		return r.deleteRBAC(ctx, app)
	}

	ns := app.Spec.Destination.Namespace
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "kubesmith-operator",
	}

	// Reconcile Owners RoleBinding
	if len(app.Spec.RBAC.Owners) > 0 {
		ownersBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesmith-owners",
				Namespace: ns,
			},
		}

		var subjects []rbacv1.Subject
		for _, owner := range app.Spec.RBAC.Owners {
			subjects = append(subjects, rbacv1.Subject{
				Kind:     "User",
				Name:     owner,
				APIGroup: "rbac.authorization.k8s.io",
			})
		}

		err := r.Client.Get(ctx, types.NamespacedName{Name: ownersBinding.Name, Namespace: ns}, ownersBinding)
		if err != nil {
			if apierrors.IsNotFound(err) {
				ownersBinding.Labels = labels
				ownersBinding.RoleRef = rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "admin",
				}
				ownersBinding.Subjects = subjects
				if err := r.Client.Create(ctx, ownersBinding); err != nil {
					return err
				}
				r.Recorder.Eventf(app, corev1.EventTypeNormal, "RBACConfigured", "Created owners RoleBinding in namespace %s", ns)
			} else {
				return err
			}
		} else {
			ownersBinding.Labels = labels
			ownersBinding.Subjects = subjects
			ownersBinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "admin",
			}
			if err := r.Client.Update(ctx, ownersBinding); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "RBACConfigured", "Updated owners RoleBinding in namespace %s", ns)
		}
	} else {
		// If no owners, delete the binding if it exists
		binding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesmith-owners",
				Namespace: ns,
			},
		}
		if err := r.Client.Delete(ctx, binding); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	// Reconcile Viewers RoleBinding
	if len(app.Spec.RBAC.Viewers) > 0 {
		viewersBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesmith-viewers",
				Namespace: ns,
			},
		}

		var subjects []rbacv1.Subject
		for _, viewer := range app.Spec.RBAC.Viewers {
			subjects = append(subjects, rbacv1.Subject{
				Kind:     "User",
				Name:     viewer,
				APIGroup: "rbac.authorization.k8s.io",
			})
		}

		err := r.Client.Get(ctx, types.NamespacedName{Name: viewersBinding.Name, Namespace: ns}, viewersBinding)
		if err != nil {
			if apierrors.IsNotFound(err) {
				viewersBinding.Labels = labels
				viewersBinding.RoleRef = rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "view",
				}
				viewersBinding.Subjects = subjects
				if err := r.Client.Create(ctx, viewersBinding); err != nil {
					return err
				}
				r.Recorder.Eventf(app, corev1.EventTypeNormal, "RBACConfigured", "Created viewers RoleBinding in namespace %s", ns)
			} else {
				return err
			}
		} else {
			viewersBinding.Labels = labels
			viewersBinding.Subjects = subjects
			viewersBinding.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "view",
			}
			if err := r.Client.Update(ctx, viewersBinding); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "RBACConfigured", "Updated viewers RoleBinding in namespace %s", ns)
		}
	} else {
		binding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesmith-viewers",
				Namespace: ns,
			},
		}
		if err := r.Client.Delete(ctx, binding); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *ApplicationReconciler) deleteRBAC(ctx context.Context, app *platformv1alpha1.Application) error {
	ns := app.Spec.Destination.Namespace

	ownersBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubesmith-owners",
			Namespace: ns,
		},
	}
	if err := r.Client.Delete(ctx, ownersBinding); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	viewersBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubesmith-viewers",
			Namespace: ns,
		},
	}
	if err := r.Client.Delete(ctx, viewersBinding); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}
