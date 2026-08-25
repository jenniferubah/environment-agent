// Package config handles configuration loading from environment variables.
package config

import (
	"fmt"
	"time"

	env "github.com/caarlos0/env/v11"
	"github.com/dcm-project/environment-agent/internal/openshift/shared"
)

// ProviderConfig holds the embedded SP identity used for NATS publishing and labeling.
type ProviderConfig struct {
	Name string `env:"SP_NAME" envDefault:"container-sp"`
}

// KubernetesConfig holds Kubernetes-specific settings.
type KubernetesConfig struct {
	Namespace           string `env:"NAMESPACE"            envDefault:"default"`
	Kubeconfig          string `env:"KUBECONFIG"`
	ExternalServiceType string `env:"EXTERNAL_SVC_TYPE"` // Must be NodePort or LoadBalancer
}

// MonitoringConfig holds status monitoring settings.
type MonitoringConfig struct {
	DebounceMs   int           `env:"DEBOUNCE_MS"   envDefault:"500"`
	ResyncPeriod time.Duration `env:"RESYNC_PERIOD" envDefault:"10m"`
}

// Config is the root configuration for the embedded container service provider.
type Config struct {
	Provider   ProviderConfig
	Kubernetes KubernetesConfig
	Monitoring MonitoringConfig
	NATSURL    string
}

// Load reads container SP configuration from environment variables.
// shared carries agent-level messaging URL and default kubeconfig.
func Load(shared shared.Config) (*Config, error) {
	if shared.MessagingURL == "" {
		return nil, fmt.Errorf("messaging URL is required")
	}

	cfg := &Config{}
	if err := env.ParseWithOptions(&cfg.Kubernetes, env.Options{Prefix: "SP_K8S_"}); err != nil {
		return nil, fmt.Errorf("loading kubernetes config: %w", err)
	}
	if err := env.ParseWithOptions(&cfg.Monitoring, env.Options{Prefix: "SP_MONITOR_"}); err != nil {
		return nil, fmt.Errorf("loading monitoring config: %w", err)
	}
	if err := env.ParseWithOptions(&cfg.Provider, env.Options{}); err != nil {
		return nil, fmt.Errorf("loading provider config: %w", err)
	}
	cfg.NATSURL = shared.MessagingURL

	if cfg.Kubernetes.Kubeconfig == "" {
		cfg.Kubernetes.Kubeconfig = shared.Kubeconfig
	}

	if err := cfg.validateKubernetes(); err != nil {
		return nil, fmt.Errorf("loading configuration: %w", err)
	}
	return cfg, nil
}

func (c *Config) validateKubernetes() error {
	switch c.Kubernetes.ExternalServiceType {
	case "LoadBalancer", "NodePort":
		return nil
	default:
		return fmt.Errorf(
			"invalid SP_K8S_EXTERNAL_SVC_TYPE %q: must be LoadBalancer or NodePort",
			c.Kubernetes.ExternalServiceType,
		)
	}
}
