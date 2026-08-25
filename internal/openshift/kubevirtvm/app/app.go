// Package app wires the KubeVirt VM service provider for in-process (embedded) use.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/config"
	"github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/events"
	"github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/kubevirt"
	kubevirtmonitor "github.com/dcm-project/environment-agent/internal/openshift/kubevirtvm/monitor"
	"github.com/dcm-project/environment-agent/internal/openshift/worker"
)

// App holds wired domain services and optional background workers.
type App struct {
	client    *kubevirt.Client
	mapper    *kubevirt.Mapper
	publisher *events.Publisher
	monitor   *kubevirtmonitor.Service
	logger    *slog.Logger
	lifecycle worker.AppLifecycle
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

	client, err := kubevirt.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubevirt client: %w", err)
	}

	a := &App{
		client:    client,
		mapper:    kubevirt.NewMapper(cfg.Namespace),
		logger:    logger,
		lifecycle: worker.NewAppLifecycle(logger),
	}

	if opts.DisableMonitor || !cfg.EventsEnabled {
		return a, nil
	}

	publisher, err := events.NewPublisher(events.PublisherConfig{
		NATSURL:      cfg.MessagingURL,
		Subject:      cfg.NATSSubject,
		MaxReconnect: cfg.NATSMaxReconnect,
	})
	if err != nil {
		return nil, fmt.Errorf("creating VM event publisher: %w", err)
	}

	monitorCfg := kubevirtmonitor.MonitorConfig{
		Namespace:    cfg.Namespace,
		ResyncPeriod: cfg.EventsResyncPeriod,
	}
	a.publisher = publisher
	a.lifecycle.RegisterCloser(publisher)
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
	a.lifecycle.Start(ctx, func(taskCtx context.Context) error {
		if a.monitor == nil {
			return nil
		}
		return a.monitor.Run(taskCtx)
	}, "VM monitor failed")
}

// Close stops the event monitor and releases resources such as the NATS connection.
func (a *App) Close() error {
	return a.lifecycle.Close()
}
