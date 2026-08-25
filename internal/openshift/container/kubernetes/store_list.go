package kubernetes

import (
	"context"
	"encoding/base64"
	"sort"
	"strconv"

	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/dcm"
	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultPageSize = 50

// List returns a paginated list of containers.
//
// Known limitation (REQ-K8S-250): Pagination uses application-level offsets
// instead of Kubernetes continue tokens. This approach fetches ALL DCM-managed
// Deployments and slices in-memory. It won't scale to large clusters and may
// produce inconsistent results between pages if containers are added or removed
// between paginated requests. Plan: migrate to metav1.ListOptions{Limit, Continue}
// when moving beyond fake client tests.
func (s *K8sContainerStore) List(ctx context.Context, maxPageSize int32, pageToken string) (*v1alpha1.ContainerList, error) {
	if maxPageSize <= 0 {
		maxPageSize = defaultPageSize
	}

	offset, err := decodePageToken(pageToken)
	if err != nil {
		return nil, err
	}

	deploys, err := s.client.AppsV1().Deployments(s.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: dcmSelector(),
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(deploys.Items, func(i, j int) bool {
		return deploys.Items[i].Name < deploys.Items[j].Name
	})

	total := len(deploys.Items)
	if offset > total {
		offset = total
	}
	paged := deploys.Items[offset:]

	limit := min(int(maxPageSize), len(paged))
	paged = paged[:limit]

	containers := make([]v1alpha1.Container, 0, len(paged))
	for i := range paged {
		deploy := &paged[i]
		instanceID := deploy.Labels[dcm.LabelInstanceID]
		c, err := s.buildContainer(ctx, deploy, instanceID)
		if err != nil {
			return nil, err
		}

		containers = append(containers, *c)
	}

	result := &v1alpha1.ContainerList{
		Containers: &containers,
	}
	if offset+limit < total {
		token := base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
		result.NextPageToken = &token
	}

	return result, nil
}

// decodePageToken parses a base64-encoded page token into an offset.
func decodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, &store.InvalidArgumentError{Message: "invalid page_token"}
	}

	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, &store.InvalidArgumentError{Message: "invalid page_token"}
	}

	return offset, nil
}
