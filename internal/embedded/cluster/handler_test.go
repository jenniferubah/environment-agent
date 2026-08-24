package cluster_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	clusterapi "github.com/dcm-project/environment-agent/api/cluster/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/embedded/cluster"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/service"
	"github.com/dcm-project/environment-agent/internal/routing"
)

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Embedded Cluster Suite")
}

type fakeClusterService struct {
	createErr error
	deleteErr error
	lastID    string
	lastSpec  clusterapi.Cluster
}

func (f *fakeClusterService) Create(_ context.Context, id string, c clusterapi.Cluster) (*clusterapi.Cluster, error) {
	f.lastID = id
	f.lastSpec = c
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &clusterapi.Cluster{Id: &id}, nil
}

func (f *fakeClusterService) Delete(_ context.Context, id string) error {
	f.lastID = id
	return f.deleteErr
}

var _ = Describe("Handler", func() {
	var (
		svc     *fakeClusterService
		handler routing.EmbeddedHandler
	)

	BeforeEach(func() {
		svc = &fakeClusterService{}
		handler = cluster.NewClusterHandler(svc, nil)
	})

	It("creates a cluster from the inbound spec", func() {
		spec, err := json.Marshal(clusterapi.Cluster{
			Spec: clusterapi.ClusterSpec{
				Metadata: clusterapi.ClusterMetadata{Name: "test-cluster"},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		err = handler.CreateResource(context.Background(), routing.CreateResourceRequest{
			ResourceID:  "cluster-1",
			ServiceType: cluster.ServiceType,
			Spec:        spec,
			EventID:     "ce-1",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.lastID).To(Equal("cluster-1"))
		Expect(svc.lastSpec.Spec.Metadata.Name).To(Equal("test-cluster"))
	})

	It("maps domain errors to SP response errors", func() {
		svc.createErr = service.NewNotFoundError("missing")

		err := handler.CreateResource(context.Background(), routing.CreateResourceRequest{
			ResourceID:  "cluster-1",
			ServiceType: cluster.ServiceType,
			Spec:        json.RawMessage(`{"spec":{"metadata":{"name":"x"}}}`),
			EventID:     "ce-1",
		})
		Expect(err).To(BeAssignableToTypeOf(&routing.SPResponseError{}))
		spErr := err.(*routing.SPResponseError)
		Expect(spErr.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("deletes by resource ID", func() {
		err := handler.DeleteResource(context.Background(), routing.DeleteResourceRequest{
			ResourceID:  "cluster-1",
			ServiceType: cluster.ServiceType,
			EventID:     "ce-2",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.lastID).To(Equal("cluster-1"))
	})

	It("detects cluster in AGENT_EMBEDDED_SPS", func() {
		Expect(cluster.Enabled([]string{"container", "cluster"})).To(BeTrue())
		Expect(cluster.Enabled([]string{"container"})).To(BeFalse())
		Expect(cluster.Enabled(nil)).To(BeFalse())
	})
})
