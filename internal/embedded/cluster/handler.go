// Package cluster embeds the ACM cluster service provider in the agent.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	clusterapi "github.com/dcm-project/environment-agent/api/cluster/v1alpha1"
	acmapp "github.com/dcm-project/environment-agent/internal/openshift/acmcluster/app"
	"github.com/dcm-project/environment-agent/internal/routing"
)

// ServiceType is the embedded SP identifier for the cluster service provider.
const ServiceType = "cluster"

// clusterLifecycle is the subset of service.ClusterService the embedded handler needs.
type clusterLifecycle interface {
	Create(ctx context.Context, id string, cluster clusterapi.Cluster) (*clusterapi.Cluster, error)
	Delete(ctx context.Context, id string) error
}

// clusterHandler implements routing.EmbeddedHandler for in-process cluster lifecycle.
type clusterHandler struct {
	lifecycle clusterLifecycle
	logger    *slog.Logger
}

var _ routing.EmbeddedHandler = (*clusterHandler)(nil)

// NewClusterHandler creates an embedded cluster handler.
func NewClusterHandler(lifecycle clusterLifecycle, logger *slog.Logger) routing.EmbeddedHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &clusterHandler{lifecycle: lifecycle, logger: logger}
}

func (h *clusterHandler) CreateResource(ctx context.Context, req routing.CreateResourceRequest) error {
	cluster, err := parseCreateCluster(req.Spec)
	if err != nil {
		return &routing.SPResponseError{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}

	_, err = h.lifecycle.Create(ctx, req.ResourceID, cluster)
	if err != nil {
		h.logger.Warn("embedded cluster create failed",
			"resource_id", req.ResourceID, "ce_id", req.EventID, "error", err)
		return mapServiceError(err)
	}
	return nil
}

func (h *clusterHandler) DeleteResource(ctx context.Context, req routing.DeleteResourceRequest) error {
	err := h.lifecycle.Delete(ctx, req.ResourceID)
	if err != nil {
		h.logger.Warn("embedded cluster delete failed",
			"resource_id", req.ResourceID, "ce_id", req.EventID, "error", err)
		return mapServiceError(err)
	}
	return nil
}

func parseCreateCluster(raw json.RawMessage) (clusterapi.Cluster, error) {
	if len(raw) == 0 {
		return clusterapi.Cluster{}, fmt.Errorf("spec is required")
	}

	var cluster clusterapi.Cluster
	if err := json.Unmarshal(raw, &cluster); err != nil {
		return clusterapi.Cluster{}, fmt.Errorf("invalid cluster spec: %w", err)
	}
	return cluster, nil
}

func mapServiceError(err error) error {
	opErr := acmapp.MapOperationError(err)
	if opErr == nil {
		return nil
	}
	return &routing.SPResponseError{StatusCode: opErr.StatusCode, Message: opErr.Message}
}
