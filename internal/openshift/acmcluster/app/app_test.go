package app_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/app"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/config"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/service"
	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/util"
	"github.com/dcm-project/environment-agent/internal/openshift/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Suite")
}

var _ = Describe("PrepareConfig", func() {
	It("returns error when cfg is nil", func() {
		err := app.PrepareConfig(nil)
		Expect(err).To(MatchError("config is required"))
	})

	It("derives pull secret name and default version matrix", func() {
		cfg := &config.Config{
			Config:  shared.Config{Name: "acm-cluster-sp"},
			Cluster: config.ClusterConfig{},
		}

		Expect(app.PrepareConfig(cfg)).To(Succeed())
		Expect(cfg.Cluster.PullSecretName).To(Equal("acm-cluster-sp-pull-secret"))
		Expect(cfg.Cluster.VersionMatrix).NotTo(BeEmpty())
	})
})

var _ = Describe("New", func() {
	It("returns error when PullSecretName is empty", func() {
		cfg := &config.Config{
			Config:  shared.Config{Name: "acm-cluster-sp"},
			Cluster: config.ClusterConfig{},
		}

		_, err := app.New(context.Background(), cfg, nil, app.Options{})
		Expect(err).To(MatchError("cluster pull secret name is empty"))
	})

	It("uses injected Kubernetes client when monitor and registration are disabled", func() {
		cfg := &config.Config{
			Config: shared.Config{Name: "acm-cluster-sp"},
			Cluster: config.ClusterConfig{
				ClusterNamespace: "clusters",
				PullSecret:       "eyJhdXRocyI6eyJjbG91ZC5vcGVuc2hpZnQuY29tIjp7ImF1dGgiOiJkR1Z6ZEE9PSJ9fX0=",
			},
		}
		Expect(app.PrepareConfig(cfg)).To(Succeed())

		scheme, err := util.BuildScheme()
		Expect(err).NotTo(HaveOccurred())
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		a, err := app.New(context.Background(), cfg, nil, app.Options{
			KubernetesClient: k8sClient,
			DisableMonitor:   true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(a.ClusterService()).NotTo(BeNil())
		Expect(a.HealthChecker()).NotTo(BeNil())
		Expect(a.Close()).To(Succeed())
	})

	It("requires RestConfig when Kubernetes client is injected and monitor is enabled", func() {
		cfg := &config.Config{
			Config: shared.Config{Name: "acm-cluster-sp", MessagingURL: "nats://127.0.0.1:4222"},
			Cluster: config.ClusterConfig{
				ClusterNamespace: "clusters",
				PullSecret:       "eyJhdXRocyI6eyJjbG91ZC5vcGVuc2hpZnQuY29tIjp7ImF1dGgiOiJkR1Z6ZEE9PSJ9fX0=",
			},
		}
		Expect(app.PrepareConfig(cfg)).To(Succeed())

		scheme, err := util.BuildScheme()
		Expect(err).NotTo(HaveOccurred())
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		_, err = app.New(context.Background(), cfg, nil, app.Options{
			KubernetesClient: k8sClient,
		})
		Expect(err).To(MatchError("RestConfig is required when KubernetesClient is set and status monitor is enabled"))
	})
})

var _ = Describe("config.Load", func() {
	It("uses agent messaging URL for NATS publishing", func() {
		GinkgoT().Setenv("SP_CLUSTER_NAMESPACE", "clusters")
		GinkgoT().Setenv("SP_PULL_SECRET", "c2VjcmV0")

		cfg, err := config.Load(shared.Agent{MessagingURL: "nats://agent:4222"})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.MessagingURL).To(Equal("nats://agent:4222"))
		Expect(cfg.Name).To(Equal("acm-cluster-sp"))
	})
})

var _ = Describe("MapOperationError", func() {
	It("maps ALREADY_EXISTS to 409", func() {
		opErr := app.MapOperationError(service.NewAlreadyExistsError("cluster exists"))
		Expect(opErr).NotTo(BeNil())
		Expect(opErr.StatusCode).To(Equal(http.StatusConflict))
		Expect(opErr.Message).To(Equal("cluster exists"))
	})
})
