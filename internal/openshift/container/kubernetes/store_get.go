package kubernetes

import (
	"context"

	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
)

// Get retrieves a container by its instance ID, enriching it with runtime
// data from Pods and Services.
func (s *K8sContainerStore) Get(ctx context.Context, containerID string) (*v1alpha1.Container, error) {
	deploy, err := s.findDeployment(ctx, containerID)
	if err != nil {
		return nil, err
	}
	return s.buildContainer(ctx, deploy, containerID)
}
