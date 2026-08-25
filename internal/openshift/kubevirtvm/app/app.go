// Package app wires the KubeVirt VM service provider for in-process (embedded) use.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/config"
	"github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/events"
	"github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/kubevirt"
	kubevirtmonitor "github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/monitor"
	"github.com/dcm-project/environment-agent/internal/openshift/worker"
)

// App holds wired domain services and optional background workers.
type App struct {
	client     *kubevirt.Client
	mapper     *kubevirt.Mapper
	publisher  *events.Publisher
	monitor    *kubevirtmonitor.Service
	logger     *slog.Logger
	background worker.Background

	closeOnce sync.Once
}

// Options configures App construction and background lifecycle.
type Options struct {
	// DisableMonitor skips VM event monitoring and the NATS publisher.
	DisableMonitor bool
}

// New constructs an App: KubeVirt client, mapper, and optional event monitor.
func New(_ context.Context, cfg *config.Config, logger *slog.Logger, opts Options) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	client, err := kubevirt.NewClient(cfg.KubernetesConfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubevirt client: %w", err)
	}

	a := &App{
		client: client,
		mapper: kubevirt.NewMapper(cfg.KubernetesConfig.Namespace),
		logger: logger,
	}

	if opts.DisableMonitor || !cfg.EventConfig.Enabled {
		return a, nil
	}

	publisher, err := events.NewPublisher(events.PublisherConfig{
		NATSURL:      cfg.NATSConfig.URL,
		Subject:      cfg.NATSConfig.Subject,
		MaxReconnect: cfg.NATSConfig.MaxReconnect,
	})
	if err != nil {
		return nil, fmt.Errorf("creating VM event publisher: %w", err)
	}

	monitorCfg := kubevirtmonitor.MonitorConfig{
		Namespace:    cfg.KubernetesConfig.Namespace,
		ResyncPeriod: cfg.EventConfig.ResyncPeriod,
	}
	a.publisher = publisher
	a.monitor = kubevirtmonitor.NewMonitorService(client.DynamicClient(), publisher, monitorCfg)
	return a, nil
}

// Client returns the KubeVirt API client.
func (a *App) Client() *kubevirt.Client {
	return a.client
}

// Mapper returns the VMSpec → VirtualMachine mapper.
func (a *App) Mapper() *kubevirt.Mapper {
	return a.mapper
}

// Start launches background workers (event monitor). It is non-blocking.
func (a *App) Start(ctx context.Context) {
	a.background.Start(ctx, func(taskCtx context.Context) error {
		if a.monitor == nil {
			return nil
		}
		return a.monitor.Run(taskCtx)
	}, func(err error) {
		a.logger.Error("VM monitor failed", "error", err)
	})
}

// Close stops the event monitor and releases resources such as the NATS connection.
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
