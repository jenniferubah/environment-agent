// Package app wires the ACM cluster service provider for in-process (embedded) use.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/cluster"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/cluster/dispatcher"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/config"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/health"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/monitoring"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/service"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/util"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/version"
	"github.com/dcm-project/environment-agent/internal/openshift/kubeconfig"
	"github.com/dcm-project/environment-agent/internal/openshift/worker"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// App holds wired domain services and optional background workers.
type App struct {
	cfg            *config.Config
	clusterService service.ClusterService
	healthChecker  service.HealthChecker
	monitor        *monitoring.StatusMonitor
	publisher      *monitoring.NATSPublisher
	logger         *slog.Logger
	background     worker.Background

	closeOnce sync.Once
}

// PrepareConfig loads derived configuration values that are not set directly
// from environment variables: the OCP→K8s compatibility matrix and the shared
// pull secret name.
func PrepareConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	matrix, err := version.LoadCompatibilityMatrix(cfg.Cluster.VersionMatrixPath)
	if err != nil {
		return fmt.Errorf("loading compatibility matrix: %w", err)
	}
	cfg.Cluster.VersionMatrix = map[string]string(matrix)
	cfg.Cluster.PullSecretName = cfg.Registration.ProviderName + "-pull-secret"
	return nil
}

// New constructs an App: Kubernetes clients, cluster service, health checker,
// and the status monitor. cfg must already have passed config.Load and PrepareConfig.
// Kubernetes clients may be injected via Options; when unset, kubeconfig is loaded automatically.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger, opts Options) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Cluster.PullSecretName == "" {
		return nil, fmt.Errorf("cluster pull secret name is empty")
	}

	restCfg, k8sClient, err := resolveKubernetesClients(cfg.Kubernetes.Kubeconfig, opts)
	if err != nil {
		return nil, err
	}

	if err := cluster.EnsurePullSecret(ctx, k8sClient, cfg.Cluster, logger); err != nil {
		return nil, fmt.Errorf("ensuring pull secret: %w", err)
	}

	a := &App{
		cfg:            cfg,
		clusterService: dispatcher.New(k8sClient, cfg.Cluster, cfg.Health.EnabledPlatforms),
		healthChecker:  health.NewChecker(k8sClient, cfg.Health, opts.version(), time.Now()),
		logger:         logger,
	}

	if err := a.initMonitor(restCfg, opts); err != nil {
		_ = a.Close()
		return nil, err
	}

	return a, nil
}

func resolveKubernetesClients(kubeconfigPath string, opts Options) (*rest.Config, client.Client, error) {
	if opts.KubernetesClient != nil {
		if opts.DisableMonitor || opts.RestConfig != nil {
			return opts.RestConfig, opts.KubernetesClient, nil
		}
		return nil, nil, fmt.Errorf("RestConfig is required when KubernetesClient is set and status monitor is enabled")
	}

	restCfg, err := kubeconfig.RESTConfig(kubeconfigPath)
	if err != nil {
		return nil, nil, err
	}

	scheme, err := util.BuildScheme()
	if err != nil {
		return nil, nil, fmt.Errorf("building scheme: %w", err)
	}

	k8sClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return restCfg, k8sClient, nil
}

func (a *App) initMonitor(restCfg *rest.Config, opts Options) error {
	if opts.DisableMonitor {
		return nil
	}

	dynamicClient := opts.DynamicClient
	if dynamicClient == nil {
		if restCfg == nil {
			return fmt.Errorf("rest config is required for status monitor")
		}
		var err error
		dynamicClient, err = dynamic.NewForConfig(restCfg)
		if err != nil {
			return fmt.Errorf("creating dynamic kubernetes client: %w", err)
		}
	}

	publisher, err := monitoring.NewNATSPublisher(
		a.cfg.Monitoring.NATSUrl,
		a.cfg.Registration.ProviderName,
		a.logger,
	)
	if err != nil {
		return fmt.Errorf("creating NATS publisher: %w", err)
	}
	a.publisher = publisher

	monitorCfg := monitoring.MonitorConfig{
		Namespace:            a.cfg.Cluster.ClusterNamespace,
		ProviderName:         a.cfg.Registration.ProviderName,
		DebounceInterval:     a.cfg.Monitoring.DebounceInterval,
		ResyncInterval:       a.cfg.Monitoring.ResyncInterval,
		PublishRetryMax:      a.cfg.Monitoring.PublishRetryMax,
		PublishRetryInterval: a.cfg.Monitoring.PublishRetryInterval,
	}
	a.monitor = monitoring.New(dynamicClient, monitorCfg, publisher, a.logger)
	return nil
}

// Config returns the app configuration. Callers must not mutate it.
func (a *App) Config() *config.Config {
	return a.cfg
}

// ClusterService returns the in-process cluster lifecycle service.
func (a *App) ClusterService() service.ClusterService {
	return a.clusterService
}

// HealthChecker returns the dependency health checker.
func (a *App) HealthChecker() service.HealthChecker {
	return a.healthChecker
}

// Start launches background workers (status monitor). It is non-blocking.
func (a *App) Start(ctx context.Context) {
	a.background.Start(ctx, func(taskCtx context.Context) error {
		if a.monitor == nil {
			return nil
		}
		return a.monitor.Start(taskCtx)
	}, func(err error) {
		a.logger.Error("status monitor failed", "error", err)
	})
}

// Close stops the status monitor and releases resources such as the NATS connection.
// It is safe to call multiple times.
func (a *App) Close() error {
	var err error
	a.closeOnce.Do(func() {
		var closers []worker.Closer
		if a.publisher != nil {
			closers = append(closers, a.publisher)
		}
		err = worker.Close(&a.background, closers...)
	})
	return err
}
