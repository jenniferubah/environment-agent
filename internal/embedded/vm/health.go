package vm

import (
	"context"

	"github.com/dcm-project/environment-agent/internal/health/monitor"
)

// vmHealthChecker checks backing KubeVirt connectivity for agent SP health.
type vmHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

func newHealthChecker(checker vmHealthChecker) monitor.Checker {
	return monitor.NewEmbeddedChecker(func(ctx context.Context) monitor.HealthCheckResult {
		if err := checker.CheckHealth(ctx); err != nil {
			return monitor.CheckUnhealthy
		}
		return monitor.CheckHealthy
	})
}
