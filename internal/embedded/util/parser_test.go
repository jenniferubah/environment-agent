package util_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/embedded/util"
)

func TestParser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Embedded Util Parser Suite")
}

var _ = Describe("SpecJSON", func() {
	It("requires non-empty input", func() {
		_, err := util.SpecJSON(nil)
		Expect(err).To(MatchError("spec is required"))
	})

	It("unwraps a wrapped spec", func() {
		raw := json.RawMessage(`{"spec":{"metadata":{"name":"x"}}}`)
		payload, err := util.SpecJSON(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(payload)).To(Equal(`{"metadata":{"name":"x"}}`))
	})

	It("returns bare spec bytes unchanged", func() {
		raw := json.RawMessage(`{"metadata":{"name":"x"}}`)
		payload, err := util.SpecJSON(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(payload).To(Equal(raw))
	})
})
