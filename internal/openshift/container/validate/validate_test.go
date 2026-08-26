package validate_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	containerapi "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/dcm"
	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	"github.com/dcm-project/environment-agent/internal/openshift/container/validate"
)

var _ = Describe("ValidateCreate", func() {
	validSpec := func() containerapi.ContainerSpec {
		return containerapi.ContainerSpec{
			Metadata: containerapi.ContainerMetadata{Name: "my-container"},
			Resources: containerapi.ContainerResources{
				Cpu:    containerapi.ContainerCpu{Min: 1, Max: 2},
				Memory: containerapi.ContainerMemory{Min: "1GB", Max: "2GB"},
			},
		}
	}

	It("accepts a valid spec", func() {
		Expect(validate.ValidateCreate("container-1", validSpec())).To(Succeed())
	})

	It("rejects reserved container ID health", func() {
		err := validate.ValidateCreate("health", validSpec())
		Expect(err).To(BeAssignableToTypeOf(&store.InvalidArgumentError{}))
		Expect(err.Error()).To(ContainSubstring("reserved"))
	})

	It("rejects cpu.min greater than cpu.max", func() {
		spec := validSpec()
		spec.Resources.Cpu = containerapi.ContainerCpu{Min: 4, Max: 1}
		err := validate.ValidateCreate("container-1", spec)
		Expect(err).To(BeAssignableToTypeOf(&store.InvalidArgumentError{}))
		Expect(err.Error()).To(ContainSubstring("cpu.min"))
	})

	It("rejects invalid memory format", func() {
		spec := validSpec()
		spec.Resources.Memory.Min = "not-memory"
		err := validate.ValidateCreate("container-1", spec)
		Expect(err).To(BeAssignableToTypeOf(&store.InvalidArgumentError{}))
		Expect(err.Error()).To(ContainSubstring("memory.min"))
	})

	It("rejects reserved DCM labels", func() {
		spec := validSpec()
		labels := map[string]string{dcm.LabelManagedBy: "user"}
		spec.Metadata.Labels = &labels
		err := validate.ValidateCreate("container-1", spec)
		Expect(err).To(BeAssignableToTypeOf(&store.InvalidArgumentError{}))
		Expect(err.Error()).To(ContainSubstring("reserved by DCM"))
	})
})
