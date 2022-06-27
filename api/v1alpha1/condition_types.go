package v1alpha1

const (
	// SecretFoundReason signals that a given secret has been found.
	SecretFoundReason string = "SecretFound"
	// WaitingForSecretReason signals that a given secret has not been found.
	WaitingForSecretReason string = "WaitingForSecret"
	// CAPIClusterFoundReason signals that a given CAPI cluster has been found.
	CAPIClusterFoundReason string = "CAPIClusterFound"
	// WaitingForCAPIClusterReason signals that a given CAPI cluster has not been found.
	WaitingForCAPIClusterReason string = "WaitingForCAPICluster"

	// WaitingForCAPIClusterDeletionReason signals that this cluster has been
	// deleted, but the referenced CAPI Cluster still exists.
	WaitingForCAPIClusterDeletionReason string = "WaitingForCAPIClusterDeletion"

	// WaitingForSecretDeletionReason signals that this cluster has been
	// deleted, but the referenced secret still exists.
	WaitingForSecretDeletionReason string = "WaitingForSecretDeletion"
)
