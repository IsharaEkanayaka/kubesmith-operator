package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/kubesmith/operator/api/v1alpha1"
)

func (r *ApplicationReconciler) reconcileNetworkPolicy(ctx context.Context, app *platformv1alpha1.Application) error {
	if app.Spec.Network == nil {
		return r.deleteNetworkPolicies(ctx, app)
	}

	ns := app.Spec.Destination.Namespace
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "kubesmith-operator",
	}

	if app.Spec.Network.DenyAll {
		denyPolicy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesmith-default-deny",
				Namespace: ns,
			},
		}

		err := r.Get(ctx, types.NamespacedName{Name: denyPolicy.Name, Namespace: denyPolicy.Namespace}, denyPolicy)
		if err != nil && apierrors.IsNotFound(err) {
			denyPolicy.Labels = labels
			denyPolicy.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			}
			if err := r.Create(ctx, denyPolicy); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "NetworkPolicyConfigured", "Created default deny NetworkPolicy %s", denyPolicy.Name)
		} else if err == nil {
			denyPolicy.Labels = labels
			denyPolicy.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{},
			}
			if err := r.Update(ctx, denyPolicy); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "NetworkPolicyConfigured", "Updated default deny NetworkPolicy %s", denyPolicy.Name)
		} else {
			return err
		}
	}

	if len(app.Spec.Network.AllowFromNamespaces) > 0 {
		allowPolicy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubesmith-allow-from",
				Namespace: ns,
			},
		}

		err := r.Get(ctx, types.NamespacedName{Name: allowPolicy.Name, Namespace: allowPolicy.Namespace}, allowPolicy)
		if err != nil && apierrors.IsNotFound(err) {
			allowPolicy.Labels = labels
			allowPolicy.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "kubernetes.io/metadata.name",
											Operator: metav1.LabelSelectorOpIn,
											Values:   app.Spec.Network.AllowFromNamespaces,
										},
									},
								},
							},
						},
					},
				},
			}
			if err := r.Create(ctx, allowPolicy); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "NetworkPolicyConfigured", "Created allow from NetworkPolicy %s", allowPolicy.Name)
		} else if err == nil {
			allowPolicy.Labels = labels
			allowPolicy.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						From: []networkingv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "kubernetes.io/metadata.name",
											Operator: metav1.LabelSelectorOpIn,
											Values:   app.Spec.Network.AllowFromNamespaces,
										},
									},
								},
							},
						},
					},
				},
			}
			if err := r.Update(ctx, allowPolicy); err != nil {
				return err
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "NetworkPolicyConfigured", "Updated allow from NetworkPolicy %s", allowPolicy.Name)
		} else {
			return err
		}
	}

	return nil
}

func (r *ApplicationReconciler) deleteNetworkPolicies(ctx context.Context, app *platformv1alpha1.Application) error {
	ns := app.Spec.Destination.Namespace

	policies := []string{"kubesmith-default-deny", "kubesmith-allow-from"}
	for _, name := range policies {
		policy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		}
		if err := r.Delete(ctx, policy); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}
