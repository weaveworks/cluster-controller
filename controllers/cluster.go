package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	deprecatedControlPlaneLabel = "node-role.kubernetes.io/master"
	controlPlaneLabel           = "node-role.kubernetes.io/control-plane"
)

// IsControlPlaneReady takes a client connected to a cluster and reports whether or
// not the control-plane for the cluster is "ready".
func IsControlPlaneReady(ctx context.Context, cl client.Client) (bool, error) {
	logger := log.FromContext(ctx)
	readiness := []bool{}
	readyNodes, err := listReadyNodesWithLabel(ctx, logger, cl, controlPlaneLabel)
	if err != nil {
		return false, err
	}
	readiness = append(readiness, readyNodes...)

	if len(readyNodes) == 0 {
		readyNodes, err := listReadyNodesWithLabel(ctx, logger, cl, deprecatedControlPlaneLabel)
		if err != nil {
			return false, err
		}
		readiness = append(readiness, readyNodes...)
	}

	isReady := func(bools []bool) bool {
		for _, v := range bools {
			if !v {
				return false
			}
		}
		return true
	}
	logger.Info("readiness", "len", len(readiness), "is-ready", isReady(readiness))

	// If we have no statuses, then we really don't know if we're ready or not.
	return (len(readiness) > 0 && isReady(readiness)), nil
}

func listReadyNodesWithLabel(ctx context.Context, logger logr.Logger, cl client.Client, label string) ([]bool, error) {
	nodes := &corev1.NodeList{}
	// https://github.com/kubernetes/enhancements/blob/master/keps/sig-cluster-lifecycle/kubeadm/2067-rename-master-label-taint/README.md#design-details
	err := cl.List(ctx, nodes, client.HasLabels([]string{label}))
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster node list: %w", err)
	}
	logger.Info("listed nodes with control plane label", "label", label, "count", len(nodes.Items))

	readiness := []bool{}
	for _, node := range nodes.Items {
		for _, c := range node.Status.Conditions {
			switch c.Type {
			case corev1.NodeReady:
				readiness = append(readiness, c.Status == corev1.ConditionTrue)
			}
		}
	}
	return readiness, nil
}
