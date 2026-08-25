// Package config handles configuration loading for the embedded kubevirt VM service provider.
package config

import (
	"fmt"
	"time"

	"github.com/dcm-project/environment-agent/internal/openshift/shared"
	"github.com/kelseyhightower/envconfig"
)

// KubernetesConfig holds configuration for connecting to Kubernetes/KubeVirt.
type KubernetesConfig struct {
	// Kubeconfig path for connecting to Kubernetes cluster (optional, defaults to in-cluster).
	Kubeconfig string `envconfig:"KUBERNETES_KUBECONFIG"`
	// Namespace for creating VMs.
	Namespace string `envconfig:"KUBERNETES_NAMESPACE" default:"default"`
	// Timeout for Kubernetes API requests.
	Timeout time.Duration `envconfig:"KUBERNETES_TIMEOUT" default:"60s"`
	// MaxRetries for failed operations.
	MaxRetries int `envconfig:"KUBERNETES_MAX_RETRIES" default:"3"`
}

// NATSConfig holds configuration for NATS connection.
type NATSConfig struct {
	// URL is the agent NATS URL (AGENT_MESSAGING_URL); not loaded from env.
	URL string `envconfig:"-"`
	// MaxReconnect attempts (-1 for unlimited).
	MaxReconnect int `envconfig:"NATS_MAX_RECONNECT" default:"-1"`
	// Subject is the JetStream subject for VM events.
	Subject string `envconfig:"NATS_SUBJECT" default:"dcm.vm"`
}

// EventConfig holds configuration for event monitoring.
type EventConfig struct {
	// Enabled controls whether event monitoring is active.
	Enabled bool `envconfig:"EVENTS_ENABLED" default:"true"`
	// ResyncPeriod for Kubernetes informers.
	ResyncPeriod time.Duration `envconfig:"EVENTS_RESYNC_PERIOD" default:"30m"`
}

// Config is the root configuration for the embedded VM service provider.
type Config struct {
	KubernetesConfig *KubernetesConfig
	NATSConfig       *NATSConfig
	EventConfig      *EventConfig
}

// Load reads VM SP configuration from environment variables.
// shared carries agent-level messaging URL and default kubeconfig.
func Load(shared shared.Config) (*Config, error) {
	if shared.MessagingURL == "" {
		return nil, fmt.Errorf("messaging URL is required")
	}

	cfg := &Config{
		KubernetesConfig: &KubernetesConfig{},
		NATSConfig:       &NATSConfig{},
		EventConfig:      &EventConfig{},
	}
	if err := envconfig.Process("", cfg); err != nil {
		return nil, err
	}
	cfg.NATSConfig.URL = shared.MessagingURL

	if cfg.KubernetesConfig.Kubeconfig == "" {
		cfg.KubernetesConfig.Kubeconfig = shared.Kubeconfig
	}

	return cfg, nil
}
