package container

import (
	"fmt"
	"sort"
	"strings"

	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/dcm"
	"github.com/dcm-project/environment-agent/internal/openshift/container/units"
)

// validationError holds a validation failure detail and an optional JSON Pointer
// (RFC 6901 §6 fragment) identifying the offending request body field.
// Pointer is empty when the error cannot be attributed to a single body field.
type validationError struct {
	Detail  string
	Pointer string
}

const (
	ptrContainerID = "#/spec/id"
	ptrCPUMin      = "#/spec/resources/cpu/min"
	ptrCPUMax      = "#/spec/resources/cpu/max"
	ptrMemMin      = "#/spec/resources/memory/min"
	ptrMemMax      = "#/spec/resources/memory/max"
)

func jsonPointerEscape(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

func labelPointer(key string) string {
	return "#/spec/metadata/labels/" + jsonPointerEscape(key)
}

func validateContainerID(id string) *validationError {
	if id == "health" {
		return &validationError{
			Detail:  fmt.Sprintf("container ID %q is reserved and cannot be used", id),
			Pointer: ptrContainerID,
		}
	}
	return nil
}

func validateResources(res v1alpha1.ContainerResources) []validationError {
	var errs []validationError

	if res.Cpu.Min > res.Cpu.Max {
		errs = append(errs, validationError{
			Detail:  fmt.Sprintf("cpu.min (%d) must not exceed cpu.max (%d)", res.Cpu.Min, res.Cpu.Max),
			Pointer: ptrCPUMin,
		})
	}

	minMem, minErr := units.ConvertMemory(res.Memory.Min)
	if minErr != nil {
		errs = append(errs, validationError{
			Detail:  fmt.Sprintf("invalid memory.min %q: %v", res.Memory.Min, minErr),
			Pointer: ptrMemMin,
		})
	}
	maxMem, maxErr := units.ConvertMemory(res.Memory.Max)
	if maxErr != nil {
		errs = append(errs, validationError{
			Detail:  fmt.Sprintf("invalid memory.max %q: %v", res.Memory.Max, maxErr),
			Pointer: ptrMemMax,
		})
	}
	if minErr == nil && maxErr == nil && minMem.Cmp(maxMem) > 0 {
		errs = append(errs, validationError{
			Detail:  fmt.Sprintf("memory.min (%s) must not exceed memory.max (%s)", res.Memory.Min, res.Memory.Max),
			Pointer: ptrMemMin,
		})
	}

	return errs
}

func validateUserLabels(labels *map[string]string) []validationError {
	if labels == nil {
		return nil
	}

	keys := make([]string, 0, len(dcm.ReservedLabelKeys))
	for k := range dcm.ReservedLabelKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var errs []validationError
	for _, k := range keys {
		if _, ok := (*labels)[k]; ok {
			errs = append(errs, validationError{
				Detail:  fmt.Sprintf("label %q is reserved by DCM and cannot be set by the user", k),
				Pointer: labelPointer(k),
			})
		}
	}
	return errs
}
