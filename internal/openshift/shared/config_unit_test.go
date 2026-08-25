package shared_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/shared"
)

var _ = Describe("Config", Label("unit"), func() {
	AfterEach(func() {
		_ = os.Unsetenv("SP_KUBECONFIG")
		_ = os.Unsetenv("SP_NAME")
	})

	It("requires agent messaging URL", func() {
		cfg := shared.Config{}
		err := shared.Apply(&cfg, shared.Agent{})
		Expect(err).To(MatchError("messaging URL is required"))
	})

	It("uses agent kubeconfig when SP_KUBECONFIG is unset", func() {
		cfg := shared.Config{}
		err := shared.LoadInto(&cfg, shared.Agent{
			MessagingURL: "nats://agent:4222",
			Kubeconfig:   "/etc/agent/kubeconfig",
		}, "test-sp")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.MessagingURL).To(Equal("nats://agent:4222"))
		Expect(cfg.Kubeconfig).To(Equal("/etc/agent/kubeconfig"))
		Expect(cfg.Name).To(Equal("test-sp"))
	})

	It("prefers SP_KUBECONFIG over agent kubeconfig", func() {
		GinkgoT().Setenv("SP_KUBECONFIG", "/sp/kubeconfig")

		cfg := shared.Config{}
		err := shared.LoadInto(&cfg, shared.Agent{
			MessagingURL: "nats://agent:4222",
			Kubeconfig:   "/agent/kubeconfig",
		}, "test-sp")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Kubeconfig).To(Equal("/sp/kubeconfig"))
	})

	It("uses SP_NAME when set", func() {
		GinkgoT().Setenv("SP_NAME", "custom-sp")

		cfg := shared.Config{}
		err := shared.LoadInto(&cfg, shared.Agent{MessagingURL: "nats://agent:4222"}, "default-sp")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Name).To(Equal("custom-sp"))
	})
})
