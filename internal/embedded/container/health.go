package container

import (
	"context"

	"github.com/dcm-project/environment-agent/internal/health/monitor"
)

// containerHealthChecker checks backing Kubernetes connectivity for agent SP health.
type containerHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

func newHealthChecker(checker containerHealthChecker) monitor.Checker {
	return monitor.NewEmbeddedChecker(func(ctx context.Context) monitor.HealthCheckResult {
		if err := checker.CheckHealth(ctx); err != nil {
			return monitor.CheckUnhealthy
		}
		return monitor.CheckHealthy
	})
}
