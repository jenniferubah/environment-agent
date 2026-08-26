// Package app wires the container service provider for in-process (embedded) use.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
	"github.com/dcm-project/environment-agent/internal/openshift/container/kubernetes"
	"github.com/dcm-project/environment-agent/internal/openshift/container/monitoring"
	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	"github.com/dcm-project/environment-agent/internal/openshift/worker"
)

// App holds wired domain services and optional background workers.
type App struct {
	cfg       *config.Config
	repo      store.ContainerRepository
	publisher *monitoring.NATSPublisher
	monitor   *monitoring.StatusMonitor
	logger    *slog.Logger
	lifecycle worker.AppLifecycle
}

// Options configures App construction and background lifecycle.
type Options struct {
	// DisableMonitor skips the Deployment/Pod status monitor and NATS publisher.
	DisableMonitor bool
}

// New constructs an App: Kubernetes client, container store, and status monitor.
func New(_ context.Context, cfg *config.Config, logger *slog.Logger, opts Options) (*App, error) {
	if cfg == nil {
		panic("container app: config must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	a := &App{cfg: cfg, logger: logger, lifecycle: worker.NewAppLifecycle(logger)}

	if opts.DisableMonitor {
		repo, err := a.buildStore()
		if err != nil {
			return nil, err
		}
		a.repo = repo
		return a, nil
	}

	publisher, err := monitoring.NewNATSPublisher(cfg.MessagingURL, cfg.Name, logger)
	if err != nil {
		return nil, fmt.Errorf("creating NATS publisher: %w", err)
	}

	k8sClient, err := kubernetes.NewClient(cfg.Kubeconfig)
	if err != nil {
		_ = publisher.Close()
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	k8sCfg := kubernetes.K8sConfig{
		Namespace:           cfg.Namespace,
		ExternalServiceType: cfg.ExternalServiceType,
	}
	repo := kubernetes.NewK8sContainerStore(k8sClient, k8sCfg, logger)

	monitorCfg := monitoring.MonitorConfig{
		Namespace:    cfg.Namespace,
		ProviderName: cfg.Name,
		DebounceMs:   cfg.DebounceMs,
		ResyncPeriod: cfg.ResyncPeriod,
	}
	statusMonitor := monitoring.NewStatusMonitor(k8sClient, monitorCfg, publisher, logger)

	a.repo = repo
	a.publisher = publisher
	a.lifecycle.RegisterCloser(publisher)
	a.monitor = statusMonitor
	return a, nil
}

func (a *App) buildStore() (store.ContainerRepository, error) {
	k8sClient, err := kubernetes.NewClient(a.cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	k8sCfg := kubernetes.K8sConfig{
		Namespace:           a.cfg.Namespace,
		ExternalServiceType: a.cfg.ExternalServiceType,
	}
	return kubernetes.NewK8sContainerStore(k8sClient, k8sCfg, a.logger), nil
}

// Store returns the in-process container repository.
func (a *App) Store() store.ContainerRepository {
	return a.repo
}

// Start launches background workers (status monitor). It is non-blocking.
func (a *App) Start(ctx context.Context) {
	a.lifecycle.Start(ctx, func(taskCtx context.Context) error {
		if a.monitor == nil {
			return nil
		}
		return a.monitor.Start(taskCtx)
	}, "container status monitor failed")
}

// Close stops the status monitor and releases resources such as the NATS connection.
func (a *App) Close() error {
	return a.lifecycle.Close()
}
