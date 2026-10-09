// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

func pilotWorkNumber(raw json.RawMessage, metric string) (float64, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	wanted := normalizePilotMetric(metric)
	var visit func(any) (float64, bool)
	visit = func(current any) (float64, bool) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if normalizePilotMetric(key) == wanted {
					if number, ok := child.(float64); ok {
						return number, true
					}
				}
			}
			for _, child := range typed {
				if number, ok := visit(child); ok {
					return number, true
				}
			}
		case []any:
			for _, child := range typed {
				if number, ok := visit(child); ok {
					return number, true
				}
			}
		}
		return 0, false
	}
	return visit(value)
}

func normalizePilotMetric(metric string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(metric))
}

func structuredPilotJSON(raw json.RawMessage) bool {
	if !substantialPilotJSON(raw) {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	return trimmed[0] == '{' || trimmed[0] == '['
}

func pilotHasNumber(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(current any) bool {
		switch typed := current.(type) {
		case float64:
			return !math.IsNaN(typed) && !math.IsInf(typed, 0)
		case map[string]any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}

func substantialPilotJSON(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) &&
		!bytes.Equal(trimmed, []byte("\"\"")) && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("[]"))
}

// PilotDefinitionsSHA256 computes the canonical digest of complete ordered
// schema, migration, or index definitions in an evidence artifact.
func PilotDefinitionsSHA256(definitions []string) string {
	data, _ := json.Marshal(definitions)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// PilotJSONSHA256 returns a stable digest for a complete JSON definition.
// Malformed or empty definitions return an empty digest and fail validation.
func PilotJSONSHA256(raw json.RawMessage) string {
	if !substantialPilotJSON(raw) {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// ValidatePilotEvidenceForBackend validates the required pilot entries owned
// by the artifact engine. Use ValidatePilotEvidenceSet for the full gate.
func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftBytes, _ := json.Marshal(leftValue)
	rightBytes, _ := json.Marshal(rightValue)
	return bytes.Equal(leftBytes, rightBytes)
}

func validatePilotParameters(parameters map[string]json.RawMessage, fixtureSHA string) error {
	if !isSHA256(fixtureSHA) || len(parameters) == 0 {
		return errors.New("safe fixture parameters and fixture identity required")
	}
	for name, raw := range parameters {
		lower := strings.ToLower(name)
		if name == "" || len(raw) > 16384 || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || !json.Valid(raw) {
			return fmt.Errorf("unsafe or malformed parameter %q", name)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil || !safePilotValue(value) {
			return fmt.Errorf("unsafe fixture value for %q", name)
		}
	}
	return nil
}

func safePilotValue(value any) bool {
	switch typed := value.(type) {
	case nil, bool, float64:
		return true
	case string:
		lower := strings.ToLower(typed)
		return len(typed) <= 512 && !strings.Contains(lower, "-----begin") && !strings.Contains(lower, "api_key") && !strings.Contains(lower, "password=")
	case []any:
		if len(typed) > 1024 {
			return false
		}
		for _, item := range typed {
			if !safePilotValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
