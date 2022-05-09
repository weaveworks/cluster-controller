package controllers_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-controller/controllers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	testName      = "test-cluster"
	testNamespace = "testing"
)

func TestReconcile(t *testing.T) {
	tests := []struct {
		name              string
		runtimeObjects    []runtime.Object
		gitopsCluster     *gitopsv1alpha1.GitopsCluster
		expectedCondition *metav1.Condition
		obj               types.NamespacedName
		requeueAfter      time.Duration
		errString         string
	}{
		{
			name: "secret does not exist",
			gitopsCluster: makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "missing",
				}
			}),
			expectedCondition: &metav1.Condition{Type: "Ready", Status: "False"},
			obj:               types.NamespacedName{Namespace: testNamespace, Name: testName},
			requeueAfter:      controllers.MissingSecretRequeueTime,
		},
		{
			name: "secret exists",
			gitopsCluster: makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "dev",
				}
			}),
			expectedCondition: &metav1.Condition{Type: "Ready", Status: "True"},
			runtimeObjects: []runtime.Object{
				makeTestSecret(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, map[string][]byte{"value": []byte("testing")}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
		},
		{
			name:              "CAPI cluster does not exist",
			expectedCondition: &metav1.Condition{Type: "Ready", Status: "False"},
			gitopsCluster: makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "missing",
				}
			}),
			obj:       types.NamespacedName{Namespace: testNamespace, Name: testName},
			errString: "failed to get CAPI cluster.*missing.*not found",
		},
		{
			name: "CAPI cluster exists but no secret",
			gitopsCluster: makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "dev",
				}
			}),
			runtimeObjects: []runtime.Object{
				makeTestCAPICluster(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}),
			},
			obj:       types.NamespacedName{Namespace: testNamespace, Name: testName},
			errString: `failed to create client of cluster testing/test-cluster: unable to read KubeConfig secret "testing/dev-kubeconfig"`,
		},
		{
			name: "Control plane not ready",
			gitopsCluster: makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "dev",
				}
			}),
			runtimeObjects: []runtime.Object{
				makeTestCAPICluster(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}),
				makeTestSecret(types.NamespacedName{
					Name:      "dev-kubeconfig",
					Namespace: testNamespace,
				}, map[string][]byte{"value": []byte("foo")}),
			},
			obj:          types.NamespacedName{Namespace: testNamespace, Name: testName},
			requeueAfter: time.Minute * 1,
		},
		{
			name: "Control plane ready",
			gitopsCluster: makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
					Name: "dev",
				}
			}),
			expectedCondition: &metav1.Condition{Type: "Ready", Status: "True"},
			runtimeObjects: []runtime.Object{
				makeNode(map[string]string{
					"node-role.kubernetes.io/master": "",
				}, corev1.NodeCondition{Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(),
					LastTransitionTime: metav1.Now(), Reason: "KubeletReady",
					Message: "kubelet is posting ready status"}),
				makeTestCAPICluster(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}),
				makeTestSecret(types.NamespacedName{
					Name:      "dev-kubeconfig",
					Namespace: testNamespace,
				}, map[string][]byte{"value": []byte("foo")}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, tc := makeTestClientAndScheme(t, append(tt.runtimeObjects, tt.gitopsCluster)...)
			r := makeTestReconciler(t, tc, s)
			r.ConfigParser = func(b []byte) (client.Client, error) {
				return r.Client, nil
			}

			result, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: tt.obj})

			if result.RequeueAfter != tt.requeueAfter {
				t.Fatalf("Reconcile() RequeueAfter got %v, want %v", result.RequeueAfter, tt.requeueAfter)
			}
			assertErrorMatch(t, tt.errString, err)

			assertCondition(t, tc, tt.expectedCondition)
		})
	}
}

func assertCondition(t *testing.T, c client.Client, expectedCondition *metav1.Condition) {
	found := false
	if expectedCondition != nil {
		name := types.NamespacedName{
			Namespace: testNamespace,
			Name:      testName,
		}
		var gc gitopsv1alpha1.GitopsCluster
		if err := c.Get(context.Background(), name, &gc); err != nil {
			t.Fatalf("couldn't find the cluster: %v", err)
		}
		for _, c := range gc.Status.Conditions {
			if c.Type == expectedCondition.Type {
				found = true
			}
			got := c.Status
			want := expectedCondition.Status
			if got != want {
				t.Fatalf("status did not match, got %s, want %s", got, want)
			}
		}
		if !found {
			t.Fatalf("did not find condition %s", expectedCondition.Type)
		}
	}
}

func makeTestReconciler(t *testing.T, c client.Client, s *runtime.Scheme) *controllers.GitopsClusterReconciler {
	return controllers.NewGitopsClusterReconciler(c, s)
}

func makeTestClientAndScheme(t *testing.T, objs ...runtime.Object) (*runtime.Scheme, client.Client) {
	t.Helper()
	s := runtime.NewScheme()
	assertNoError(t, clientsetscheme.AddToScheme(s))
	assertNoError(t, gitopsv1alpha1.AddToScheme(s))
	assertNoError(t, clusterv1.AddToScheme(s))
	return s, fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
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
