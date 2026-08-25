package config_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
	"github.com/dcm-project/environment-agent/internal/openshift/shared"
)

var _ = Describe("Configuration", func() {
	sharedNATS := shared.Config{MessagingURL: "nats://test:4222"}

	// Helper to unset all config-related env vars between tests.
	clearEnv := func() {
		_ = os.Unsetenv("SP_NAME")
		_ = os.Unsetenv("SP_K8S_EXTERNAL_SVC_TYPE")
		_ = os.Unsetenv("SP_K8S_KUBECONFIG")
		_ = os.Unsetenv("SP_MONITOR_DEBOUNCE_MS")
		_ = os.Unsetenv("SP_MONITOR_RESYNC_PERIOD")
	}

	BeforeEach(func() {
		clearEnv()
	})

	AfterEach(func() {
		clearEnv()
	})

	It("loads configuration from environment variables", func() {
		_ = os.Setenv("SP_NAME", "test-sp")
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "LoadBalancer")
		_ = os.Setenv("SP_MONITOR_DEBOUNCE_MS", "250")
		_ = os.Setenv("SP_MONITOR_RESYNC_PERIOD", "5m")

		cfg, err := config.Load(sharedNATS)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Provider.Name).To(Equal("test-sp"))
		Expect(cfg.NATSURL).To(Equal("nats://test:4222"))
		Expect(cfg.Kubernetes.ExternalServiceType).To(Equal("LoadBalancer"))
		Expect(cfg.Monitoring.DebounceMs).To(Equal(250))
		Expect(cfg.Monitoring.ResyncPeriod).To(Equal(5 * time.Minute))
	})

	It("defaults provider name and monitoring settings", func() {
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "NodePort")

		cfg, err := config.Load(sharedNATS)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Provider.Name).To(Equal("container-sp"))
		Expect(cfg.Monitoring.DebounceMs).To(Equal(500))
		Expect(cfg.Monitoring.ResyncPeriod).To(Equal(10 * time.Minute))
	})

	It("returns error when messaging URL is missing", func() {
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "NodePort")

		cfg, err := config.Load(shared.Config{})
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("messaging URL is required"))
	})

	It("returns error when SP_K8S_EXTERNAL_SVC_TYPE is not set", func() {
		cfg, err := config.Load(sharedNATS)
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid SP_K8S_EXTERNAL_SVC_TYPE"))
	})

	It("rejects invalid SP_K8S_EXTERNAL_SVC_TYPE values", func() {
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "ClusterIP")

		cfg, err := config.Load(sharedNATS)
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("must be LoadBalancer or NodePort"))
	})

	It("uses agent kubeconfig when SP_K8S_KUBECONFIG is unset", func() {
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "NodePort")

		cfg, err := config.Load(shared.Config{
			MessagingURL: "nats://test:4222",
			Kubeconfig:   "/etc/agent/kubeconfig",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Kubernetes.Kubeconfig).To(Equal("/etc/agent/kubeconfig"))
	})
})
