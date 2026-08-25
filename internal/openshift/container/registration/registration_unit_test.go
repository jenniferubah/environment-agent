package registration_test

import (
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
	"github.com/dcm-project/environment-agent/internal/openshift/container/registration"
)

var _ = Describe("Registration Payload", func() {
	// TC-U043: Payload contains all configured fields
	It("contains name, serviceType, displayName, endpoint with suffix, and operations (TC-U043)", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
		}

		payload := registration.BuildPayload(cfg)

		Expect(payload.Name).To(Equal("k8s-sp"))
		Expect(payload.ServiceType).To(Equal("container"))
		Expect(payload.DisplayName).To(HaveValue(Equal("K8s Container SP")))
		Expect(payload.Endpoint).To(Equal("https://sp.example.com/api/v1alpha1/containers"))
		Expect(payload.Operations).To(HaveValue(ConsistOf("CREATE", "DELETE", "READ")))
		Expect(payload.SchemaVersion).To(Equal("v1alpha1"))
	})

	// TC-U044: Payload includes region_code and zone metadata when configured
	It("includes metadata.region_code and metadata.zone when configured (TC-U044)", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
				Region:      "us-east-1",
				Zone:        "us-east-1a",
			},
		}

		payload := registration.BuildPayload(cfg)

		Expect(payload.Metadata).NotTo(BeNil())
		Expect(payload.Metadata.RegionCode).To(HaveValue(Equal("us-east-1")))
		Expect(payload.Metadata.Zone).To(HaveValue(Equal("us-east-1a")))
	})

	// TC-U045: Payload omits metadata when region/zone absent
	It("omits metadata when region and zone are not configured (TC-U045)", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
		}

		payload := registration.BuildPayload(cfg)

		// Guard: verify payload is constructed (name must be set)
		Expect(payload.Name).To(Equal("k8s-sp"))
		// Then metadata should be nil
		Expect(payload.Metadata).To(BeNil())
	})

	// TC-U064: Payload omits display_name when not configured
	It("omits display_name when not configured (TC-U064)", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:     "k8s-sp",
				Endpoint: "https://sp.example.com",
			},
		}

		payload := registration.BuildPayload(cfg)

		Expect(payload.Name).To(Equal("k8s-sp"))
		Expect(payload.DisplayName).To(BeNil())
	})

	// TC-U061: NewRegistrar returns error for invalid registration URL
	It("returns error for invalid registration URL (TC-U061)", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: "://invalid-url",
			},
		}
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("creating DCM client"))
		Expect(registrar).To(BeNil())
	})

	// TC-U062: BuildPayload values do not alias config memory
	It("payload values do not alias config memory (TC-U062)", func() {
		cfg := &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "Original",
				Endpoint:    "https://sp.example.com",
				Region:      "us-east-1",
				Zone:        "us-east-1a",
			},
		}

		payload := registration.BuildPayload(cfg)

		// Mutate config after building payload.
		cfg.Provider.DisplayName = "Mutated"
		cfg.Provider.Region = "eu-west-1"
		cfg.Provider.Zone = "eu-west-1b"

		// Payload values must remain unchanged.
		Expect(payload.DisplayName).To(HaveValue(Equal("Original")))
		Expect(payload.Metadata.RegionCode).To(HaveValue(Equal("us-east-1")))
		Expect(payload.Metadata.Zone).To(HaveValue(Equal("us-east-1a")))
	})
})
