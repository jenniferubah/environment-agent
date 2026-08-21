package config_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
)

var _ = Describe("Configuration", func() {
	// Helper to unset all config-related env vars between tests.
	clearEnv := func() {
		_ = os.Unsetenv("SP_SERVER_ADDRESS")
		_ = os.Unsetenv("SP_SERVER_SHUTDOWN_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_READ_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_WRITE_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_IDLE_TIMEOUT")
		_ = os.Unsetenv("SP_NAME")
		_ = os.Unsetenv("SP_DISPLAY_NAME")
		_ = os.Unsetenv("SP_ENDPOINT")
		_ = os.Unsetenv("SP_REGION")
		_ = os.Unsetenv("SP_ZONE")
		_ = os.Unsetenv("DCM_REGISTRATION_URL")
		_ = os.Unsetenv("SP_NATS_URL")
		_ = os.Unsetenv("SP_SERVER_REQUEST_TIMEOUT")
		_ = os.Unsetenv("SP_K8S_EXTERNAL_SVC_TYPE")
	}

	BeforeEach(func() {
		clearEnv()
	})

	AfterEach(func() {
		clearEnv()
	})

	// setRequiredEnv sets the mandatory env vars so Load() succeeds.
	setRequiredEnv := func() {
		_ = os.Setenv("SP_NAME", "test-sp")
		_ = os.Setenv("SP_ENDPOINT", "http://sp")
		_ = os.Setenv("DCM_REGISTRATION_URL", "http://dcm")
		_ = os.Setenv("SP_NATS_URL", "nats://test:4222")
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "LoadBalancer")
	}

	// TC-U002: Load configuration from environment variables
	It("loads configuration from environment variables (TC-U002)", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_SERVER_ADDRESS", ":9090")
		_ = os.Setenv("SP_SERVER_SHUTDOWN_TIMEOUT", "30s")
		_ = os.Setenv("SP_DISPLAY_NAME", "Test Provider")
		_ = os.Setenv("SP_REGION", "us-east-1")
		_ = os.Setenv("SP_ZONE", "us-east-1a")
		_ = os.Setenv("SP_SERVER_READ_TIMEOUT", "10s")
		_ = os.Setenv("SP_SERVER_WRITE_TIMEOUT", "20s")
		_ = os.Setenv("SP_SERVER_IDLE_TIMEOUT", "120s")
		_ = os.Setenv("SP_SERVER_REQUEST_TIMEOUT", "45s")

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())
		Expect(cfg.Server.Address).To(Equal(":9090"))
		Expect(cfg.Server.ShutdownTimeout).To(Equal(30 * time.Second))
		Expect(cfg.Provider.Name).To(Equal("test-sp"))
		Expect(cfg.Provider.DisplayName).To(Equal("Test Provider"))
		Expect(cfg.Provider.Endpoint).To(Equal("http://sp"))
		Expect(cfg.Server.ReadTimeout).To(Equal(10 * time.Second))
		Expect(cfg.Server.WriteTimeout).To(Equal(20 * time.Second))
		Expect(cfg.Server.IdleTimeout).To(Equal(120 * time.Second))
		Expect(cfg.Server.RequestTimeout).To(Equal(45 * time.Second))
		Expect(cfg.Provider.Region).To(Equal("us-east-1"))
		Expect(cfg.Provider.Zone).To(Equal("us-east-1a"))
		Expect(cfg.DCM.RegistrationURL).To(Equal("http://dcm"))
	})

	// TC-U004: Default values applied when no config specified
	It("applies default values when no config is specified (TC-U004)", func() {
		setRequiredEnv()

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())
		Expect(cfg.Server.Address).To(Equal(":8080"))
		Expect(cfg.Server.ShutdownTimeout).To(Equal(15 * time.Second))
		Expect(cfg.Server.ReadTimeout).To(Equal(15 * time.Second))
		Expect(cfg.Server.WriteTimeout).To(Equal(15 * time.Second))
		Expect(cfg.Server.IdleTimeout).To(Equal(60 * time.Second))
		Expect(cfg.Server.RequestTimeout).To(Equal(30 * time.Second))
	})

	// TC-U063: Load returns error when required fields are missing
	It("returns error when required fields are missing (TC-U063)", func() {
		// All required env vars unset (clearEnv called in BeforeEach).
		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())

		errMsg := err.Error()
		Expect(errMsg).To(HavePrefix("loading configuration:"))
		Expect(errMsg).To(ContainSubstring("SP_NAME"))
		Expect(errMsg).To(ContainSubstring("SP_ENDPOINT"))
		Expect(errMsg).To(ContainSubstring("DCM_REGISTRATION_URL"))
		Expect(errMsg).To(ContainSubstring("SP_NATS_URL"))
	})

	// TC-U082: ExternalServiceType is required at startup
	It("returns error when SP_K8S_EXTERNAL_SVC_TYPE is not set (TC-U082)", func() {
		setRequiredEnv()
		_ = os.Unsetenv("SP_K8S_EXTERNAL_SVC_TYPE")

		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("loading configuration: invalid SP_K8S_EXTERNAL_SVC_TYPE"))
	})

	// TC-U086: ExternalServiceType rejects empty string
	It("returns error when SP_K8S_EXTERNAL_SVC_TYPE is empty (TC-U086)", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "")

		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("loading configuration: invalid SP_K8S_EXTERNAL_SVC_TYPE"))
		Expect(err.Error()).To(ContainSubstring("must be LoadBalancer or NodePort"))
	})

	// TC-U083: ExternalServiceType rejects invalid values
	It("returns error when SP_K8S_EXTERNAL_SVC_TYPE is ClusterIP (TC-U083)", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "ClusterIP")

		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("loading configuration: invalid SP_K8S_EXTERNAL_SVC_TYPE"))
		Expect(err.Error()).To(ContainSubstring("must be LoadBalancer or NodePort"))
	})

	// TC-U084: ExternalServiceType accepts LoadBalancer
	It("loads successfully with ExternalServiceType=LoadBalancer (TC-U084)", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "LoadBalancer")

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Kubernetes.ExternalServiceType).To(Equal("LoadBalancer"))
	})

	// TC-U085: ExternalServiceType accepts NodePort
	It("loads successfully with ExternalServiceType=NodePort (TC-U085)", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "NodePort")

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Kubernetes.ExternalServiceType).To(Equal("NodePort"))
	})
})
