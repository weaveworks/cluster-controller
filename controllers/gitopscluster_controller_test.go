package controllers_test

import (
	"context"
	"path/filepath"
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
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	testName           = "test-cluster"
	testNamespace      = "testing"
	defaultRequeueTime = 1 * time.Minute
)

func TestReconcile(t *testing.T) {
	tests := []struct {
		name              string
		state             []runtime.Object
		obj               types.NamespacedName
		opts              controllers.Options
		requeueAfter      time.Duration
		errString         string
		wantCondition     string
		wantStatus        metav1.ConditionStatus
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
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      controllers.MissingSecretRequeueTime,
			wantCondition:     meta.ReadyCondition,
			wantStatus:        "False",
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
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      defaultRequeueTime,
			wantCondition:     meta.ReadyCondition,
			wantStatus:        "True",
			wantStatusMessage: "Referenced secret is available",
		},
		{
			name: "non-CAPI cluster has provisioned annotation",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.ObjectMeta.Annotations = map[string]string{
						controllers.GitOpsClusterProvisionedAnnotation: "true",
					}
					c.Spec.SecretRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			// The referenced secret doesn't exist so we should still check for
			// it.
			requeueAfter:      controllers.MissingSecretRequeueTime,
			wantCondition:     gitopsv1alpha1.ClusterProvisionedCondition,
			wantStatus:        "True",
			wantStatusMessage: "Cluster Provisioned annotation detected",
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
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			errString:         "failed to get CAPI cluster.*missing.*not found",
			wantCondition:     meta.ReadyCondition,
			wantStatus:        "False",
			wantStatusMessage: "failed to get CAPI cluster \"testing/missing\": clusters.cluster.x-k8s.io \"missing\" not found",
		},
		{
			name: "CAPI cluster exists but is not ready",
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
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      defaultRequeueTime,
			wantCondition:     meta.ReadyCondition,
			wantStatus:        "False",
			wantStatusMessage: "Waiting for ControlPlaneReady status",
		},
		{
			name: "CAPI cluster exists and is provisioned",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestCAPICluster(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, func(c *clusterv1.Cluster) {
					c.Status.SetTypedPhase(clusterv1.ClusterPhaseProvisioned)
				}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      defaultRequeueTime,
			wantCondition:     gitopsv1alpha1.ClusterProvisionedCondition,
			wantStatus:        "True",
			wantStatusMessage: "CAPI Cluster has been provisioned",
		},
		{
			name: "CAPI cluster exists and is ready",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestCAPICluster(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, func(c *clusterv1.Cluster) {
					c.Status.ControlPlaneReady = true
					c.Status.SetTypedPhase(clusterv1.ClusterPhaseProvisioned)
				}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:  defaultRequeueTime,
			wantCondition: meta.ReadyCondition,
			wantStatus:    "True",
		},
		{
			name: "CAPI component is not enabled",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled: false,
			},
			wantCondition:     meta.ReadyCondition,
			wantStatus:        "False",
			wantStatusMessage: "CAPIClusterRef \"dev\" found but CAPI support is disabled",
		},
		{
			name: "verify connectivity to the cluster",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.SecretRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestSecret(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, map[string][]byte{"value": kubeConfig}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      defaultRequeueTime,
			wantCondition:     gitopsv1alpha1.ClusterConnectivity,
			wantStatus:        "True",
			wantStatusMessage: "cluster connectivity is ok",
		},
		{
			name: "verify connectivity failed to read secret",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.SecretRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestSecret(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, map[string][]byte{"value": []byte("foo")}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      defaultRequeueTime,
			wantCondition:     gitopsv1alpha1.ClusterConnectivity,
			wantStatus:        "False",
			wantStatusMessage: "failed creating rest config from secret: couldn't get version/kind; json parse error: json: cannot unmarshal string into Go value of type struct { APIVersion string \"json:\\\"apiVersion,omitempty\\\"\"; Kind string \"json:\\\"kind,omitempty\\\"\" }",
		},
		{
			name: "verify connectivity failed to connect",
			state: []runtime.Object{
				makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
					c.Spec.SecretRef = &meta.LocalObjectReference{
						Name: "dev",
					}
				}),
				makeTestSecret(types.NamespacedName{
					Name:      "dev",
					Namespace: testNamespace,
				}, map[string][]byte{"value": kubeconfigWithError(t)}),
			},
			obj: types.NamespacedName{Namespace: testNamespace, Name: testName},
			opts: controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			},
			requeueAfter:      defaultRequeueTime,
			wantCondition:     gitopsv1alpha1.ClusterConnectivity,
			wantStatus:        "False",
			wantStatusMessage: `failed creating rest config from secret: invalid configuration: no server found for cluster "envtest"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestReconciler(t, tt.opts, tt.state...)
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
			assertClusterStatus(t, cls, tt.wantCondition, tt.wantStatus, tt.wantStatusMessage)
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
			opts := controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			}
			r := makeTestReconciler(t, opts, append(tt.additionalObjs, tt.gitopsCluster)...)

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
			"cluster referencing CAPI cluster but capi-enabled is false",
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
			"cluster referencing secret - but no-secret-finalization annotation",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				c.ObjectMeta.Namespace = "test-ns"
				c.ObjectMeta.Annotations = map[string]string{
					gitopsv1alpha1.GitOpsClusterNoSecretFinalizerAnnotation: "true",
				}
				c.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "test-cluster",
				}
			}),
			[]runtime.Object{makeTestSecret(types.NamespacedName{Name: "test-cluster", Namespace: "test-ns"},
				map[string][]byte{"value": []byte("test")})},
			false,
		},

		{
			"deleted gitops cluster",
			makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
				now := metav1.NewTime(time.Now())
				c.ObjectMeta.Namespace = "test-ns"
				c.ObjectMeta.Finalizers = []string{"testing"}
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
			opts := controllers.Options{
				CAPIEnabled:        true,
				DefaultRequeueTime: defaultRequeueTime,
			}
			r := makeTestReconciler(t, opts, append(tt.additionalObjs, tt.gitopsCluster)...)

			_, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      tt.gitopsCluster.Name,
				Namespace: tt.gitopsCluster.Namespace,
			}})
			if err != nil {
				t.Fatal(err)
			}

			updated := testGetGitopsCluster(t, r.Client, client.ObjectKeyFromObject(tt.gitopsCluster))
			if v := controllerutil.ContainsFinalizer(updated, controllers.GitOpsClusterFinalizer); v != tt.wantFinalizer {
				t.Fatalf("cluster HasFinalizer got %v, want %v", v, tt.wantFinalizer)
			}
		})
	}
}

func TestGitopsClusterValidation(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}
	testCfg, err := testEnv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Fatalf("failed to shutdown testEnv: %s", err)
		}
	}()

	s := runtime.NewScheme()
	if err := gitopsv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	cl, err := client.New(testCfg, client.Options{Scheme: s})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("when neither the secret nor capi cluster are configured", func(t *testing.T) {
		testCluster := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
			c.ObjectMeta.Name = "no-config"
			c.ObjectMeta.Namespace = "default"
			c.Spec.SecretRef = nil
			c.Spec.CAPIClusterRef = nil
		})

		err := cl.Create(context.TODO(), testCluster)
		assertErrorMatch(t, "must provide a secretRef or capiClusterRef", err)
		assertErrorDoesNotMatch(t, "cannot provide both capiClusterRef and secretRef", err)
	})

	t.Run("when both the secret and capi cluster are configured", func(t *testing.T) {
		testCluster := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
			c.ObjectMeta.Name = "both-configs"
			c.ObjectMeta.Namespace = "default"
			c.Spec.SecretRef = &meta.LocalObjectReference{
				Name: "test-secret",
			}
			c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
				Name: "test-cluster",
			}
		})

		err := cl.Create(context.TODO(), testCluster)
		assertErrorMatch(t, "cannot provide both capiClusterRef and secretRef", err)
		assertErrorDoesNotMatch(t, "must provide a secretRef or capiClusterRef", err)
	})

	t.Run("when the secret is configured", func(t *testing.T) {
		testCluster := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
			c.ObjectMeta.Name = "only-secret-configured"
			c.ObjectMeta.Namespace = "default"
			c.Spec.SecretRef = &meta.LocalObjectReference{
				Name: "test-secret",
			}
		})

		assertNoError(t, cl.Create(context.TODO(), testCluster))
	})

	t.Run("when the capiClusterRef is configured", func(t *testing.T) {
		testCluster := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
			c.ObjectMeta.Name = "only-capi-cluster-configured"
			c.ObjectMeta.Namespace = "default"
			c.Spec.CAPIClusterRef = &meta.LocalObjectReference{
				Name: "test-cluster",
			}
		})

		assertNoError(t, cl.Create(context.TODO(), testCluster))
	})

}

func makeTestReconciler(t *testing.T, opts controllers.Options, objs ...runtime.Object) *controllers.GitopsClusterReconciler {
	s, tc := makeTestClientAndScheme(t, opts, objs...)
	return controllers.NewGitopsClusterReconciler(tc, s, opts)
}

func makeTestClientAndScheme(t *testing.T, opts controllers.Options, objs ...runtime.Object) (*runtime.Scheme, client.Client) {
	s := makeClusterScheme(t, opts)
	return s, fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&gitopsv1alpha1.GitopsCluster{}).
		WithRuntimeObjects(objs...).Build()
}

func makeClusterScheme(t *testing.T, opts controllers.Options) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	assertNoError(t, clientsetscheme.AddToScheme(s))
	assertNoError(t, gitopsv1alpha1.AddToScheme(s))

	if opts.CAPIEnabled {
		assertNoError(t, clusterv1.AddToScheme(s))
	}

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

func assertErrorDoesNotMatch(t *testing.T, s string, e error) {
	t.Helper()
	if matchErrorString(t, s, e) {
		t.Fatalf("error did match, got %s, should not contain %s", e, s)
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

func assertClusterStatus(t *testing.T, cls *gitopsv1alpha1.GitopsCluster, condType string, status metav1.ConditionStatus, msg string) {
	t.Helper()
	cond := conditions.Get(cls, condType)
	if status == "" {
		return
	}

	if cond == nil && status != "" {
		t.Fatalf("%s condition was nil", condType)
	}

	if cond.Status != status {
		t.Fatalf("got condition status %q, want %q", cond.Status, status)
	}
	if cond.Message != msg {
		t.Fatalf("got condition reason %q, want %q", cond.Message, msg)
	}
}

func kubeconfigWithError(t *testing.T) []byte {
	clientConfig, err := clientcmd.Load(kubeConfig)
	assertNoError(t, err)

	clientConfig.Clusters["envtest"].Server = ""

	data, err := clientcmd.Write(*clientConfig)
	assertNoError(t, err)

	return data
}
