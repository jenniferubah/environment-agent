package kubernetes

// K8sConfig holds configuration for the Kubernetes container store.
type K8sConfig struct {
	Namespace           string
	ExternalServiceType string
}
