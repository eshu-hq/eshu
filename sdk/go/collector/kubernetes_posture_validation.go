// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"errors"
)

// validateKubernetesPostureField admits only the producer-owned posture
// fields at the root of their version-1 facts. It checks key safety, leaving
// integer and other payload semantics to an optional payload schema.
func validateKubernetesPostureField(value any, key, kind, schema string) (bool, error) {
	if schema != "1.0.0" {
		return false, nil
	}
	account := kind == "k8s_service_account" || kind == "k8s_service_account_token_posture"
	if account {
		switch key {
		case "secret_ref_count", "image_pull_secret_ref_count":
			return true, validatePostureCount(value)
		case "automount_token":
			return true, validateAutomountState(value)
		}
	}
	if kind == "k8s_workload_identity_use" && key == "projected_service_account_token" {
		if _, ok := value.(bool); ok {
			return true, nil
		}
		switch value.(type) {
		case nil, string, json.Number, float64, float32,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			return true, errors.New("must be a boolean")
		default:
			return true, errCanonicalPayloadRequired
		}
	}
	return false, nil
}

func validatePostureCount(value any) error {
	switch value.(type) {
	case nil, json.Number, float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return nil
	case string, bool:
		return errors.New("must be a JSON number or null")
	default:
		// Aliases, pointers, typed nil containers, RawMessage and custom
		// marshalers must be checked in the exact JSON shape they emit.
		return errCanonicalPayloadRequired
	}
}

func validateAutomountState(value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if typed == "true" || typed == "false" || typed == "unknown" {
			return nil
		}
		return errors.New("must be true, false, unknown, or null")
	case bool, json.Number, float64, float32,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return errors.New("must be true, false, unknown, or null")
	default:
		return errCanonicalPayloadRequired
	}
}
