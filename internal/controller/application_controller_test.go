package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/kubesmith/operator/api/v1alpha1"
)

func setupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)
	_ = networkingv1.AddToScheme(s)
	_ = platformv1alpha1.AddToScheme(s)
	return s
}

func getCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func TestReconcile_BasicApplication(t *testing.T) {
	s := setupScheme()
	app := &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: platformv1alpha1.ApplicationSpec{
			Source: platformv1alpha1.ApplicationSource{
				RepoURL:  "https://github.com/myorg/myrepo",
				Path:     "apps/test",
				Revision: "main",
			},
			Destination: platformv1alpha1.ApplicationDestination{
				Namespace: "test-ns",
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&platformv1alpha1.Application{}).WithObjects(app).Build()
	recorder := record.NewFakeRecorder(100)

	r := &ApplicationReconciler{
		Client:   cl,
		Scheme:   s,
		Recorder: recorder,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()

	// First Reconcile: should add finalizer and requeue
	res, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.True(t, res.Requeue)

	err = cl.Get(ctx, req.NamespacedName, app)
	require.NoError(t, err)
	assert.Contains(t, app.Finalizers, applicationFinalizer)

	// Second Reconcile: should execute reconciliation
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.False(t, res.Requeue)

	// Refresh app
	err = cl.Get(ctx, req.NamespacedName, app)
	require.NoError(t, err)

	// Assert: Target Namespace "test-ns" is created with label "app.kubernetes.io/managed-by": "kubesmith-operator".
	ns := &corev1.Namespace{}
	err = cl.Get(ctx, types.NamespacedName{Name: "test-ns"}, ns)
	require.NoError(t, err)
	assert.Equal(t, "kubesmith-operator", ns.Labels["app.kubernetes.io/managed-by"])

	// Assert: Condition "NamespaceReady" has Status "True".
	condNs := getCondition(app.Status.Conditions, ConditionNamespaceReady)
	require.NotNil(t, condNs)
	assert.Equal(t, metav1.ConditionTrue, condNs.Status)

	// Assert: Condition "ArgoCDReady" has Status "True".
	condArgo := getCondition(app.Status.Conditions, ConditionArgoCDReady)
	require.NotNil(t, condArgo)
	assert.Equal(t, metav1.ConditionTrue, condArgo.Status)

	// Assert: Status.Phase is "Ready".
	assert.Equal(t, "Ready", app.Status.Phase)

	// Assert: Argo CD Application unstructured object exists in "argocd" namespace named after the app.
	argoApp := &unstructured.Unstructured{}
	argoApp.SetGroupVersionKind(mustGVK("argoproj.io", "v1alpha1", "Application"))
	err = cl.Get(ctx, types.NamespacedName{Name: "test-app", Namespace: "argocd"}, argoApp)
	require.NoError(t, err)
}

func TestReconcile_NamespaceWithQuotaAndLimitRange(t *testing.T) {
	s := setupScheme()
	app := &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app",
			Namespace:  "default",
			Finalizers: []string{applicationFinalizer},
		},
		Spec: platformv1alpha1.ApplicationSpec{
			Destination: platformv1alpha1.ApplicationDestination{
				Namespace: "test-ns",
				ResourceQuota: &platformv1alpha1.ResourceQuotaSpec{
					CPU:    "2",
					Memory: "4Gi",
					Pods:   "10",
				},
				LimitRange: &platformv1alpha1.LimitRangeSpec{
					DefaultCPU:    "250m",
					DefaultMemory: "512Mi",
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&platformv1alpha1.Application{}).WithObjects(app).Build()
	recorder := record.NewFakeRecorder(100)

	r := &ApplicationReconciler{
		Client:   cl,
		Scheme:   s,
		Recorder: recorder,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-app", Namespace: "default"},
	}

	_, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)

	ctx := context.Background()

	// Assert: corev1.ResourceQuota "kubesmith-quota" in "test-ns" has the specified limits.
	quota := &corev1.ResourceQuota{}
	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-quota", Namespace: "test-ns"}, quota)
	require.NoError(t, err)
	assert.Equal(t, resource.MustParse("2"), quota.Spec.Hard[corev1.ResourceLimitsCPU])
	assert.Equal(t, resource.MustParse("4Gi"), quota.Spec.Hard[corev1.ResourceLimitsMemory])
	assert.Equal(t, resource.MustParse("10"), quota.Spec.Hard[corev1.ResourcePods])

	// Assert: corev1.LimitRange "kubesmith-defaults" in "test-ns" has the specified container default limits.
	lr := &corev1.LimitRange{}
	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-defaults", Namespace: "test-ns"}, lr)
	require.NoError(t, err)
	require.Len(t, lr.Spec.Limits, 1)
	assert.Equal(t, corev1.LimitTypeContainer, lr.Spec.Limits[0].Type)
	assert.Equal(t, resource.MustParse("250m"), lr.Spec.Limits[0].Default[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("512Mi"), lr.Spec.Limits[0].Default[corev1.ResourceMemory])
}

func TestReconcile_RBAC(t *testing.T) {
	s := setupScheme()
	app := &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app",
			Namespace:  "default",
			Finalizers: []string{applicationFinalizer},
		},
		Spec: platformv1alpha1.ApplicationSpec{
			Destination: platformv1alpha1.ApplicationDestination{
				Namespace: "test-ns",
			},
			RBAC: &platformv1alpha1.ApplicationRBAC{
				Owners:  []string{"alice@example.com"},
				Viewers: []string{"bob@example.com"},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&platformv1alpha1.Application{}).WithObjects(app).Build()
	recorder := record.NewFakeRecorder(100)

	r := &ApplicationReconciler{
		Client:   cl,
		Scheme:   s,
		Recorder: recorder,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-app", Namespace: "default"},
	}

	ctx := context.Background()
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Assert: rbacv1.RoleBinding "kubesmith-owners" in "test-ns" has RoleRef "admin" and subject "alice@example.com".
	rbOwners := &rbacv1.RoleBinding{}
	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-owners", Namespace: "test-ns"}, rbOwners)
	require.NoError(t, err)
	assert.Equal(t, "admin", rbOwners.RoleRef.Name)
	require.Len(t, rbOwners.Subjects, 1)
	assert.Equal(t, "alice@example.com", rbOwners.Subjects[0].Name)

	// Assert: rbacv1.RoleBinding "kubesmith-viewers" in "test-ns" has RoleRef "view" and subject "bob@example.com".
	rbViewers := &rbacv1.RoleBinding{}
	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-viewers", Namespace: "test-ns"}, rbViewers)
	require.NoError(t, err)
	assert.Equal(t, "view", rbViewers.RoleRef.Name)
	require.Len(t, rbViewers.Subjects, 1)
	assert.Equal(t, "bob@example.com", rbViewers.Subjects[0].Name)

	err = cl.Get(ctx, req.NamespacedName, app)
	require.NoError(t, err)
	condRBAC := getCondition(app.Status.Conditions, ConditionRBACReady)
	require.NotNil(t, condRBAC)
	assert.Equal(t, metav1.ConditionTrue, condRBAC.Status)
}

func TestReconcile_NetworkPolicy(t *testing.T) {
	s := setupScheme()
	app := &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app",
			Namespace:  "default",
			Finalizers: []string{applicationFinalizer},
		},
		Spec: platformv1alpha1.ApplicationSpec{
			Destination: platformv1alpha1.ApplicationDestination{
				Namespace: "test-ns",
			},
			Network: &platformv1alpha1.ApplicationNetwork{
				DenyAll:             true,
				AllowFromNamespaces: []string{"ingress-nginx", "monitoring"},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&platformv1alpha1.Application{}).WithObjects(app).Build()
	recorder := record.NewFakeRecorder(100)

	r := &ApplicationReconciler{
		Client:   cl,
		Scheme:   s,
		Recorder: recorder,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-app", Namespace: "default"},
	}

	ctx := context.Background()
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Assert: networkingv1.NetworkPolicy "kubesmith-default-deny" in "test-ns" exists with empty Ingress.
	denyPol := &networkingv1.NetworkPolicy{}
	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-default-deny", Namespace: "test-ns"}, denyPol)
	require.NoError(t, err)
	assert.Empty(t, denyPol.Spec.Ingress)
	assert.Contains(t, denyPol.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)

	// Assert: networkingv1.NetworkPolicy "kubesmith-allow-from" in "test-ns" exists with namespace selector matching the allowed namespaces.
	allowPol := &networkingv1.NetworkPolicy{}
	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-allow-from", Namespace: "test-ns"}, allowPol)
	require.NoError(t, err)
	require.Len(t, allowPol.Spec.Ingress, 1)
	require.Len(t, allowPol.Spec.Ingress[0].From, 1)
	require.NotNil(t, allowPol.Spec.Ingress[0].From[0].NamespaceSelector)
	require.Len(t, allowPol.Spec.Ingress[0].From[0].NamespaceSelector.MatchExpressions, 1)

	expr := allowPol.Spec.Ingress[0].From[0].NamespaceSelector.MatchExpressions[0]
	assert.Equal(t, "kubernetes.io/metadata.name", expr.Key)
	assert.Equal(t, metav1.LabelSelectorOpIn, expr.Operator)
	assert.ElementsMatch(t, []string{"ingress-nginx", "monitoring"}, expr.Values)

	err = cl.Get(ctx, req.NamespacedName, app)
	require.NoError(t, err)
	condNet := getCondition(app.Status.Conditions, ConditionNetworkReady)
	require.NotNil(t, condNet)
	assert.Equal(t, metav1.ConditionTrue, condNet.Status)
}

func TestReconcile_Deletion(t *testing.T) {
	s := setupScheme()
	now := metav1.Now()
	app := &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-app",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{applicationFinalizer},
		},
		Spec: platformv1alpha1.ApplicationSpec{
			Destination: platformv1alpha1.ApplicationDestination{
				Namespace: "test-ns",
			},
		},
	}

	argoApp := &unstructured.Unstructured{}
	argoApp.SetGroupVersionKind(mustGVK("argoproj.io", "v1alpha1", "Application"))
	argoApp.SetName("test-app")
	argoApp.SetNamespace("argocd")

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kubesmith-owners", Namespace: "test-ns"},
	}
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "kubesmith-default-deny", Namespace: "test-ns"},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&platformv1alpha1.Application{}).
		WithObjects(app, argoApp, rb, np).Build()
	recorder := record.NewFakeRecorder(100)

	r := &ApplicationReconciler{
		Client:   cl,
		Scheme:   s,
		Recorder: recorder,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-app", Namespace: "default"},
	}

	ctx := context.Background()
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Assert finalizer is removed and object is deleted by API server / fake client
	err = cl.Get(ctx, req.NamespacedName, app)
	assert.True(t, apierrors.IsNotFound(err))

	// Assert child resources are deleted
	err = cl.Get(ctx, types.NamespacedName{Name: "test-app", Namespace: "argocd"}, argoApp)
	assert.True(t, client.IgnoreNotFound(err) == nil && err != nil)

	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-owners", Namespace: "test-ns"}, rb)
	assert.True(t, client.IgnoreNotFound(err) == nil && err != nil)

	err = cl.Get(ctx, types.NamespacedName{Name: "kubesmith-default-deny", Namespace: "test-ns"}, np)
	assert.True(t, client.IgnoreNotFound(err) == nil && err != nil)
}
