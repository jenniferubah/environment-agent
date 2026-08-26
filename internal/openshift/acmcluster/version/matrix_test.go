package version_test

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/acmcluster/version"
)

func TestVersion(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Version Suite")
}

var _ = Describe("LoadCompatibilityMatrix", func() {
	It("returns default matrix when path is empty", func() {
		matrix, err := version.LoadCompatibilityMatrix("")
		Expect(err).NotTo(HaveOccurred())
		Expect(matrix).To(Equal(version.DefaultCompatibilityMatrix))
	})

	It("loads matrix from JSON file", func() {
		content := `{"4.16": "1.29", "4.17": "1.30"}`
		tmpFile := GinkgoT().TempDir() + "/matrix.json"
		Expect(os.WriteFile(tmpFile, []byte(content), 0o644)).To(Succeed())

		matrix, err := version.LoadCompatibilityMatrix(tmpFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(matrix).To(HaveLen(2))
		Expect(matrix["4.16"]).To(Equal("1.29"))
		Expect(matrix["4.17"]).To(Equal("1.30"))
	})

	It("returns error for non-existent file", func() {
		_, err := version.LoadCompatibilityMatrix("/nonexistent/path.json")
		Expect(err).To(HaveOccurred())
	})

	It("returns error for invalid JSON", func() {
		tmpFile := GinkgoT().TempDir() + "/bad.json"
		Expect(os.WriteFile(tmpFile, []byte("not json"), 0o644)).To(Succeed())

		_, err := version.LoadCompatibilityMatrix(tmpFile)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ExtractOCPVersion", func() {
	It("extracts major.minor from a release image tag", func() {
		Expect(version.ExtractOCPVersion("quay.io/openshift-release-dev/ocp-release:4.17.3-multi")).To(Equal("4.17"))
	})

	It("returns empty string when tag is missing", func() {
		Expect(version.ExtractOCPVersion("quay.io/openshift-release-dev/ocp-release")).To(Equal(""))
	})
})
