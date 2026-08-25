// Package app wires the container service provider for in-process (embedded) use.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
	"github.com/dcm-project/environment-agent/internal/openshift/container/kubernetes"
	"github.com/dcm-project/environment-agent/internal/openshift/container/monitoring"
	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	"github.com/dcm-project/environment-agent/internal/openshift/worker"
)

// App holds wired domain services and optional background workers.
type App struct {
	cfg        *config.Config
	repo       store.ContainerRepository
	publisher  *monitoring.NATSPublisher
	monitor    *monitoring.StatusMonitor
	logger     *slog.Logger
	background worker.Background

	closeOnce sync.Once
}

// Options configures App construction and background lifecycle.
type Options struct {
	// DisableMonitor skips the Deployment/Pod status monitor and NATS publisher.
	DisableMonitor bool
}

// New constructs an App: Kubernetes client, container store, and status monitor.
func New(_ context.Context, cfg *config.Config, logger *slog.Logger, opts Options) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	a := &App{cfg: cfg, logger: logger}

	if opts.DisableMonitor {
		repo, err := a.buildStore()
		if err != nil {
			return nil, err
		}
		a.repo = repo
		return a, nil
	}

	publisher, err := monitoring.NewNATSPublisher(cfg.NATSURL, cfg.Provider.Name, logger)
	if err != nil {
		return nil, fmt.Errorf("creating NATS publisher: %w", err)
	}

	k8sClient, err := kubernetes.NewClient(cfg.Kubernetes.Kubeconfig)
	if err != nil {
		_ = publisher.Close()
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	k8sCfg := kubernetes.K8sConfig{
		Namespace:           cfg.Kubernetes.Namespace,
		ExternalServiceType: cfg.Kubernetes.ExternalServiceType,
	}
	repo := kubernetes.NewK8sContainerStore(k8sClient, k8sCfg, logger)

	monitorCfg := monitoring.MonitorConfig{
		Namespace:    cfg.Kubernetes.Namespace,
		ProviderName: cfg.Provider.Name,
		DebounceMs:   cfg.Monitoring.DebounceMs,
		ResyncPeriod: cfg.Monitoring.ResyncPeriod,
	}
	statusMonitor := monitoring.NewStatusMonitor(k8sClient, monitorCfg, publisher, logger)

	a.repo = repo
	a.publisher = publisher
	a.monitor = statusMonitor
	return a, nil
}

func (a *App) buildStore() (store.ContainerRepository, error) {
	k8sClient, err := kubernetes.NewClient(a.cfg.Kubernetes.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	k8sCfg := kubernetes.K8sConfig{
		Namespace:           a.cfg.Kubernetes.Namespace,
		ExternalServiceType: a.cfg.Kubernetes.ExternalServiceType,
	}
	return kubernetes.NewK8sContainerStore(k8sClient, k8sCfg, a.logger), nil
}

// Store returns the in-process container repository.
func (a *App) Store() store.ContainerRepository {
	return a.repo
}

// Start launches background workers (status monitor). It is non-blocking.
func (a *App) Start(ctx context.Context) {
	a.background.Start(ctx, func(taskCtx context.Context) error {
		if a.monitor == nil {
			return nil
		}
		return a.monitor.Start(taskCtx)
	}, func(err error) {
		a.logger.Error("container status monitor failed", "error", err)
	})
}

// Close stops the status monitor and releases resources such as the NATS connection.
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
