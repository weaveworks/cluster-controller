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
)
