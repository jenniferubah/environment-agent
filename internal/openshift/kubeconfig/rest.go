// Package kubeconfig builds Kubernetes REST configs for embedded SPs.
package kubeconfig

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// RESTConfig returns a REST config from kubeconfigPath when set, otherwise in-cluster config.
func RESTConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("building config from kubeconfig %q: %w", kubeconfigPath, err)
		}
		return restCfg, nil
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("loading in-cluster config: %w", err)
	}
	return restCfg, nil
}
