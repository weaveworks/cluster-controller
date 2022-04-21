package controllers_test

import (
	"context"
	"testing"

	"github.com/weaveworks/cluster-controller/controllers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIsControlPlaneReady(t *testing.T) {
	controlPlaneLabels := map[string]string{
		"node-role.kubernetes.io/master":        "",
		"node-role.kubernetes.io/control-plane": "",
		"beta.kubernetes.io/arch":               "amd64",
		"beta.kubernetes.io/os":                 "linux",
		"kubernetes.io/arch":                    "amd64",
		"kubernetes.io/hostname":                "kind-control-plane",
		"kubernetes.io/os":                      "linux",
	}

	nodeTests := []struct {
		name       string
		labels     map[string]string
		conditions []corev1.NodeCondition
		wantReady  bool
	}{
		{
			name:   "control plane not ready",
			labels: controlPlaneLabels,
			conditions: makeConditions(
				corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletNotReady", Message: "container runtime network not ready: NetworkReady=false reason:NetworkPluginNotReady message:Network plugin returns error: cni plugin not initialized"},
			),
		},
		{
			name:   "control plane ready",
			labels: controlPlaneLabels,
			conditions: makeConditions(
				corev1.NodeCondition{Type: "NetworkUnavailable", Status: "False", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "CalicoIsUp", Message: "Calico is running on this node"},
				corev1.NodeCondition{Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"},
			),
			wantReady: true,
		},
		{
			name:   "no control plane",
			labels: map[string]string{},
			conditions: makeConditions(
				corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletNotReady", Message: "container runtime network not ready: NetworkReady=false reason:NetworkPluginNotReady message:Network plugin returns error: cni plugin not initialized"},
			),
		},
	}
	for _, tt := range nodeTests {
		t.Run(tt.name, func(t *testing.T) {
			cl := makeClient(makeNode(tt.labels, tt.conditions...))

			ready, err := controllers.IsControlPlaneReady(context.TODO(), cl)
			if err != nil {
				t.Fatal(err)
			}

			if ready != tt.wantReady {
				t.Fatalf("IsControlPlaneReady() got %v, want %v", ready, tt.wantReady)
			}
		})
	}
}

func makeNode(labels map[string]string, conds ...corev1.NodeCondition) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-control-plane",
			Labels: labels,
		},
		Spec: corev1.NodeSpec{},
		Status: corev1.NodeStatus{
			Conditions: conds,
		},
	}
}

func makeClient(objs ...runtime.Object) client.Client {
	return fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
}

func makeConditions(conds ...corev1.NodeCondition) []corev1.NodeCondition {
	base := []corev1.NodeCondition{
		corev1.NodeCondition{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletHasSufficientMemory", Message: "kubelet has sufficient memory available"},
		corev1.NodeCondition{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletHasNoDiskPressure", Message: "kubelet has no disk pressure"},
		corev1.NodeCondition{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse, LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletHasSufficientPID", Message: "kubelet has sufficient PID available"},
	}
	return append(conds, base...)
}
