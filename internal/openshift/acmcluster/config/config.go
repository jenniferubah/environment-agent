// Package config provides environment-based configuration for the embedded cluster service provider.
package config

import (
	"fmt"
	"time"

	env "github.com/caarlos0/env/v11"
	"github.com/dcm-project/environment-agent/internal/openshift/shared"
)

// HealthConfig holds health endpoint settings.
type HealthConfig struct {
	CheckTimeout     time.Duration `env:"SP_HEALTH_CHECK_TIMEOUT"   envDefault:"5s"`
	EnabledPlatforms []string      `env:"SP_ENABLED_PLATFORMS"      envDefault:"kubevirt,baremetal" envSeparator:","`
}

// RegistrationConfig holds embedded SP identity fields.
type RegistrationConfig struct {
	ProviderName string `env:"SP_NAME" envDefault:"acm-cluster-sp"`
}

// KubernetesConfig holds Kubernetes client settings.
type KubernetesConfig struct {
	Kubeconfig string `env:"SP_KUBECONFIG"`
}

// ClusterConfig holds ACM cluster service configuration.
type ClusterConfig struct {
	ClusterNamespace  string            `env:"SP_CLUSTER_NAMESPACE,required"`
	BaseDomain        string            `env:"SP_BASE_DOMAIN"`
	PullSecret        string            `env:"SP_PULL_SECRET,required"` // REQ-ACM-195
	PullSecretName    string            `env:"-"`
	ConsoleURIPattern string            `env:"SP_CONSOLE_URI_PATTERN" envDefault:"https://console-openshift-console.apps.{name}.{base_domain}"`
	VersionMatrixPath string            `env:"SP_VERSION_MATRIX_PATH"`
	DefaultInfraEnv   string            `env:"SP_DEFAULT_INFRA_ENV"`
	AgentNamespace    string            `env:"SP_AGENT_NAMESPACE"`
	InfraEnvLabelKey  string            `env:"SP_INFRA_ENV_LABEL_KEY" envDefault:"infraenvs.agent-install.openshift.io"`
	VersionMatrix     map[string]string `env:"-"`
}

// MonitoringConfig holds status monitoring settings.
type MonitoringConfig struct {
	NATSUrl              string        `env:"-"`
	DebounceInterval     time.Duration `env:"SP_STATUS_DEBOUNCE_INTERVAL"     envDefault:"1s"`
	ResyncInterval       time.Duration `env:"SP_STATUS_RESYNC_INTERVAL"       envDefault:"10m"`
	PublishRetryMax      int           `env:"SP_NATS_PUBLISH_RETRY_MAX"       envDefault:"3"`
	PublishRetryInterval time.Duration `env:"SP_NATS_PUBLISH_RETRY_INTERVAL"  envDefault:"2s"`
}

// Config is the root configuration for the embedded cluster service provider.
type Config struct {
	Registration RegistrationConfig
	Health       HealthConfig
	Kubernetes   KubernetesConfig
	Cluster      ClusterConfig
	Monitoring   MonitoringConfig
}

// Load reads cluster SP configuration from environment variables.
// shared carries agent-level messaging URL and default kubeconfig.
func Load(shared shared.Config) (*Config, error) {
	if shared.MessagingURL == "" {
		return nil, fmt.Errorf("messaging URL is required")
	}

	cfg := &Config{}

	if err := env.ParseWithOptions(&cfg.Health, env.Options{}); err != nil {
		return nil, fmt.Errorf("SP health config: %w", err)
	}
	if err := env.ParseWithOptions(&cfg.Kubernetes, env.Options{}); err != nil {
		return nil, fmt.Errorf("SP kubernetes config: %w", err)
	}
	if err := env.ParseWithOptions(&cfg.Cluster, env.Options{}); err != nil {
		return nil, fmt.Errorf("SP cluster config: %w", err)
	}
	if err := env.ParseWithOptions(&cfg.Monitoring, env.Options{}); err != nil {
		return nil, fmt.Errorf("SP monitoring config: %w", err)
	}
	cfg.Monitoring.NATSUrl = shared.MessagingURL

	if err := env.ParseWithOptions(&cfg.Registration, env.Options{}); err != nil {
		return nil, fmt.Errorf("SP registration config: %w", err)
	}

	if cfg.Kubernetes.Kubeconfig == "" {
		cfg.Kubernetes.Kubeconfig = shared.Kubeconfig
	}

	return cfg, nil
}
