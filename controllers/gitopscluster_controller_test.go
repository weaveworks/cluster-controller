package controllers_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-controller/controllers"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	testName      = "test-cluster"
	testNamespace = "testing"
)

func TestReconcile(t *testing.T) {
	tests := []struct {
		name              string
		state             []runtime.Object
		obj               types.NamespacedName
		requeueAfter      time.Duration
		errString         string
		wantStatusMessage string
	}{
		{
			name: "secret does not exist",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.SecretRef = &meta.LocalObjectReference{
						Name: "missing",
					}
				}),
			},
			obj:               types.NamespacedName{Namespace: testNamespace, Name: testName},
			requeueAfter:      controllers.MissingSecretRequeueTime,
			wantStatusMessage: "failed to get secret \"testing/missing\": secrets \"missing\" not found",
		},
		{
			name: "secret exists",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.SecretRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestSecret(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, map[string][]byte{"value": []byte("testing")}),
			},
			obj:               types.NamespacedName{Namespace: testNamespace, Name: testName},
			wantStatusMessage: "",
		},
		{
			name: "CAPI cluster does not exist",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
						Name: "missing",
					}
				}),
			},
			obj:               types.NamespacedName{Namespace: testNamespace, Name: testName},
			errString:         "failed to get CAPI cluster.*missing.*not found",
			wantStatusMessage: "failed to get CAPI cluster \"testing/missing\": clusters.cluster.x-k8s.io \"missing\" not found",
		},
		{
			name: "CAPI cluster exists",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestCAPICluster(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}),
			},
			obj:               types.NamespacedName{Namespace: testNamespace, Name: testName},
			wantStatusMessage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestReconciler(t, tt.state...)
			r.ConfigParser = func(b []byte) (client.Client, error) {
				return r.Client, nil
			}

			result, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: tt.obj})

			if result.RequeueAfter != tt.requeueAfter {
				t.Fatalf("Reconcile() RequeueAfter got %v, want %v", result.RequeueAfter, tt.requeueAfter)
			}
			assertErrorMatch(t, tt.errString, err)

			clsObjectKey := types.NamespacedName{Namespace: testNamespace, Name: testName}
			cls := testGetGitopsCluster(t, r.Client, clsObjectKey)
			cond := conditions.Get(cls, meta.ReadyCondition)

			if cond != nil {
				if cond.Message != tt.wantStatusMessage {
					t.Fatalf("got condition reason %q, want %q", cond.Message, tt.wantStatusMessage)
				}
			}
		})
	}
}

func TestFinalizedDeletion(t *testing.T) {
	finalizerTests := []struct {
		name           string
		gitopsCluster  *gitopsv1alpha1.GitopsCluster
		additionalObjs []runtime.Object

		wantStatusReason string
		errString        string
		clusterExists    bool
	}{
		{
			"when CAPI cluster exists",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{makeTestCAPICluster(types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"})},
			gitopsv1alpha1.WaitingForCAPIClusterDeletionReason,
			"waiting for CAPI cluster to be deleted",
			true,
		},
		{
			"when CAPI cluster has been deleted",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{},
			"",
			"",
			false,
		},
		{
			"when referenced secret exists",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{makeTestSecret(types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"},
				map[string][]byte{"value": []byte("test")})},
			gitopsv1alpha1.WaitingForSecretDeletionReason,
			"waiting for access secret to be deleted",
			true,
		},
		{
			"when referenced secret has been deleted",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{},
			"",
			"",
			false,
		},
	}

	for _, tt := range finalizerTests {
		t.Run(tt.name, func(t *testing.T) {
			now := metav1.NewTime(time.Now())
			tt.gitopsCluster.ObjectMeta.DeletionTimestamp = &now
			controllerutil.AddFinalizer(tt.gitopsCluster, controllers.GitOpsClusterFinalizer)
			r := makeTestReconciler(t, append(tt.additionalObjs, tt.gitopsCluster)...)

			_, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      tt.gitopsCluster.Name,
				Namespace: tt.gitopsCluster.Namespace,
			}})
			assertErrorMatch(t, tt.errString, err)

			if tt.clusterExists {
				updated := testGetGitopsCluster(t, r.Client, client.ObjectKeyFromObject(tt.gitopsCluster))
				cond := conditions.Get(updated, meta.ReadyCondition)

				if cond != nil {
					if cond.Reason != tt.wantStatusReason {
						t.Fatalf("got condition reason %q, want %q", cond.Reason, tt.wantStatusReason)
					}
				}
			} else {
				var cluster gitopsv1alpha1.GitopsCluster
				err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(tt.gitopsCluster), &cluster)
				if !apierrors.IsNotFound(err) {
					t.Fatalf("expected cluster to not exist but got cluster %v", cluster)
				}
			}
		})
	}
}

func TestFinalizers(t *testing.T) {
	finalizerTests := []struct {
		name           string
		gitopsCluster  *gitopsv1alpha1.GitopsCluster
		additionalObjs []runtime.Object

		wantFinalizer bool
	}{
		{
			"when cluster has no other reference",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
			}),
			[]runtime.Object{},
			false,
		},
		{
			"cluster referencing CAPI cluster",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{makeTestCAPICluster(types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"})},
			true,
		},
		{
			"cluster referencing secret",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{makeTestSecret(types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"},
				map[string][]byte{"value": []byte("test")})},
			true,
		},
		{
			"deleted gitops cluster",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				now := metav1.NewTime(time.Now())
				c.ObjectMeta.Namespace = "test-ns"
				c.ObjectMeta.DeletionTimestamp = &now
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{makeTestCAPICluster(types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"})},
			false,
		},
	}

	for _, tt := range finalizerTests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestReconciler(t, append(tt.additionalObjs, tt.gitopsCluster)...)

			_, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      tt.gitopsCluster.Name,
				Namespace: tt.gitopsCluster.Namespace,
			}})
			if err != nil {
				t.Fatal(err)
			}

			if tt.wantFinalizer {
				updated := testGetGitopsCluster(t, r.Client, client.ObjectKeyFromObject(tt.gitopsCluster))
				if !controllerutil.ContainsFinalizer(updated, controllers.GitOpsClusterFinalizer) {
					t.Fatal("cluster HasFinalizer got false, want true")
				}
			}
		})
	}
}

func makeTestReconciler(t *testing.T, objs ...runtime.Object) controllers.GitopsClusterReconciler {
	s, tc := makeTestClientAndScheme(t, objs...)
	return controllers.GitopsClusterReconciler{
		Client: tc,
		Scheme: s,
	}
}

func makeTestClientAndScheme(t *testing.T, objs ...runtime.Object) (*runtime.Scheme, client.Client) {
	s := makeClusterScheme(t)
	return s, fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
}

func makeClusterScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	assertNoError(t, clientsetscheme.AddToScheme(s))
	assertNoError(t, gitopsv1alpha1.AddToScheme(s))
	assertNoError(t, clusterv1.AddToScheme(s))
	return s
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func makeTestCluster(opts ...func(*gitopsv1alpha1.GitopsCluster)) *gitopsv1alpha1.GitopsCluster {
	c := &gitopsv1alpha1.GitopsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: gitopsv1alpha1.GitopsClusterSpec{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func makeTestSecret(name types.NamespacedName, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Data: data,
	}
}

func makeTestCAPICluster(name types.NamespacedName, opts ...func(*clusterv1.Cluster)) *clusterv1.Cluster {
	c := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Spec: clusterv1.ClusterSpec{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func assertErrorMatch(t *testing.T, s string, e error) {
	t.Helper()
	if !matchErrorString(t, s, e) {
		t.Fatalf("error did not match, got %s, want %s", e, s)
	}
}

func matchErrorString(t *testing.T, s string, e error) bool {
	t.Helper()
	if s == "" && e == nil {
		return true
	}
	if s != "" && e == nil {
		return false
	}
	match, err := regexp.MatchString(s, e.Error())
	if err != nil {
		t.Fatal(err)
	}
	return match
}

func testGetGitopsCluster(t *testing.T, c client.Client, k client.ObjectKey) *gitopsv1alpha1.GitopsCluster {
	t.Helper()
	var cluster gitopsv1alpha1.GitopsCluster
	if err := c.Get(context.TODO(), k, &cluster); err != nil {
		t.Fatal(err)
	}
	return &cluster
}
