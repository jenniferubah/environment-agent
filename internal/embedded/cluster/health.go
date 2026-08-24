package cluster

import (
	"context"
	"strings"

	clusterapi "github.com/dcm-project/environment-agent/api/cluster/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/health/monitor"
)

// clusterHealthChecker is the subset of service.HealthChecker used for agent SP health.
type clusterHealthChecker interface {
	Check(ctx context.Context) clusterapi.Health
}

func newHealthChecker(checker clusterHealthChecker) monitor.Checker {
	return monitor.NewEmbeddedChecker(func() monitor.HealthCheckResult {
		h := checker.Check(context.Background())
		if h.Status == nil {
			return monitor.CheckFailed
		}
		switch strings.ToLower(*h.Status) {
		case "healthy":
			return monitor.CheckHealthy
		case "unhealthy":
			return monitor.CheckUnhealthy
		default:
			return monitor.CheckFailed
		}
	})
}
