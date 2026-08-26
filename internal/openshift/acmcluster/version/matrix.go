// Package version maps OpenShift release versions to Kubernetes minor versions.
package version

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CompatibilityMatrix maps OCP minor versions to K8s minor versions.
type CompatibilityMatrix map[string]string

// DefaultCompatibilityMatrix contains the OCP 4.x = K8s 1.(x+13) mappings.
var DefaultCompatibilityMatrix = CompatibilityMatrix{
	"4.14": "1.27",
	"4.15": "1.28",
	"4.16": "1.29",
	"4.17": "1.30",
	"4.18": "1.31",
	"4.19": "1.32",
	"4.20": "1.33",
	"4.21": "1.34",
}

// LoadCompatibilityMatrix loads a compatibility matrix from a JSON file.
// If path is empty, returns DefaultCompatibilityMatrix.
func LoadCompatibilityMatrix(path string) (CompatibilityMatrix, error) {
	if path == "" {
		return DefaultCompatibilityMatrix, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading compatibility matrix from %s: %w", path, err)
	}
	var matrix CompatibilityMatrix
	if err := json.Unmarshal(data, &matrix); err != nil {
		return nil, fmt.Errorf("parsing compatibility matrix from %s: %w", path, err)
	}
	return matrix, nil
}

// ExtractOCPVersion extracts the OCP major.minor from a release image reference.
// Example: "quay.io/openshift-release-dev/ocp-release:4.17.3-multi" -> "4.17"
func ExtractOCPVersion(releaseImage string) string {
	colonIdx := strings.LastIndex(releaseImage, ":")
	if colonIdx < 0 {
		return ""
	}
	tag := releaseImage[colonIdx+1:]
	parts := strings.SplitN(tag, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}
