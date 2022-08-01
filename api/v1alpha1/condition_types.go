package v1alpha1

const (
	// SecretFoundReason signals that a given secret has been found.
	SecretFoundReason string = "SecretFound"
	// WaitingForSecretReason signals that a given secret has not been found.
	WaitingForSecretReason string = "WaitingForSecret"
	// WaitingForControlPlaneReadyStatusReason  signals that a given CAPI cluster has been found but its ControlPlaneReady status is false
	WaitingForControlPlaneReadyStatusReason string = "WaitingForControlPlaneReadyStatus"
	// ControlPlaneReadyStatusReason signals that a cluster is found and it has a ControlPlaneReady=true condition
	ControlPlaneReadyStatusReason string = "ControlPlaneReadyStatus"
	// WaitingForCAPIClusterReason signals that a given CAPI cluster has not been found.
	WaitingForCAPIClusterReason string = "WaitingForCAPICluster"

	// WaitingForCAPIClusterDeletionReason signals that this cluster has been
	// deleted, but the referenced CAPI Cluster still exists.
	WaitingForCAPIClusterDeletionReason string = "WaitingForCAPIClusterDeletion"

	// WaitingForSecretDeletionReason signals that this cluster has been
	// deleted, but the referenced secret still exists.
	WaitingForSecretDeletionReason string = "WaitingForSecretDeletion"

	// CAPINotEnable signals that CAPI component is not installed.
	CAPINotEnable string = "CAPINotEnable"
)
