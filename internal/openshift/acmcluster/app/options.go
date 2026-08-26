package app

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Options configures App construction and background lifecycle.
type Options struct {
	// Version is the application version reported by the health checker.
	// When empty, defaults to "0.0.1-dev".
	Version string

	// DisableMonitor skips the HostedCluster/NodePool status monitor and NATS
	// publisher. Intended for tests or minimal deployments.
	DisableMonitor bool

	// KubernetesClient is the controller-runtime client used for cluster
	// operations and health checks. When nil, New loads kubeconfig and
	// constructs a client.
	KubernetesClient client.Client

	// RestConfig configures the status monitor's dynamic informers. Required when
	// KubernetesClient is set and DisableMonitor is false. When nil and
	// KubernetesClient is nil, New loads kubeconfig.
	RestConfig *rest.Config

	// DynamicClient watches HostedCluster and NodePool for the status monitor.
	// When nil and DisableMonitor is false, New constructs one from RestConfig.
	DynamicClient dynamic.Interface
}

func (o Options) version() string {
	if o.Version != "" {
		return o.Version
	}
	return "0.0.1-dev"
}
