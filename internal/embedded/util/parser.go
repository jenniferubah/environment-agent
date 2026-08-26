// Package util provides small helpers shared by embedded service provider handlers.
package util

import (
	"encoding/json"
	"fmt"
)

// SpecJSON returns spec bytes from either a wrapped {"spec": ...} envelope or a bare spec object.
func SpecJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("spec is required")
	}

	var wrapper struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("invalid spec: %w", err)
	}
	if wrapper.Spec != nil {
		return wrapper.Spec, nil
	}
	return raw, nil
}
