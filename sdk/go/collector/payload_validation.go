// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Unknown native values need the same key walk as their wire representation.
// MarshalJSON may produce object keys that are invisible in the Go value.
var errCanonicalPayloadRequired = errors.New("canonical JSON payload required")

func validateFactPayload(fact Fact) error {
	raw, err := json.Marshal(fact.Payload)
	if err != nil {
		return fmt.Errorf("must be JSON serializable: %w", err)
	}
	if err := walkFactPayload(fact.Payload, "", fact.Kind, fact.SchemaVersion, 0); !errors.Is(err, errCanonicalPayloadRequired) {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var canonical map[string]any
	if err := decoder.Decode(&canonical); err != nil {
		return fmt.Errorf("decode canonical payload: %w", err)
	}
	return walkFactPayload(canonical, "", fact.Kind, fact.SchemaVersion, 0)
}

func walkFactPayload(value any, path, kind, schema string, depth int) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := payloadKeyPath(path, key)
			if depth == 0 && key == "tag_value_fingerprints" && kind == "azure_tag_observation" && schema == "1.0.0" {
				if err := validateAzureTagFingerprints(child); err != nil {
					return fmt.Errorf("%s: %w", childPath, err)
				}
				continue
			}
			if depth == 0 {
				if handled, err := validateKubernetesPostureField(child, key, kind, schema); handled {
					if err != nil {
						return fmt.Errorf("%s: %w", childPath, err)
					}
					continue
				}
			}
			if err := rejectSensitivePayloadKey(key, childPath); err != nil {
				return err
			}
			if err := walkFactPayload(child, childPath, kind, schema, depth+1); err != nil {
				return err
			}
		}
	case map[string]string:
		for key := range typed {
			if err := rejectSensitivePayloadKey(key, payloadKeyPath(path, key)); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := walkFactPayload(child, path, kind, schema, depth+1); err != nil {
				return err
			}
		}
	case []string, nil, string, bool, time.Time, json.Number,
		float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		// These values cannot conceal JSON object keys after Marshal succeeds.
	default:
		return errCanonicalPayloadRequired
	}
	return nil
}

func validateAzureTagFingerprints(value any) error {
	switch fingerprints := value.(type) {
	case map[string]string:
		if fingerprints == nil {
			return errors.New("must be an object")
		}
		for key, fingerprint := range fingerprints {
			if !validAzureTagMarker(fingerprint) {
				return fmt.Errorf("invalid marker for tag %q", key)
			}
		}
	case map[string]any:
		if fingerprints == nil {
			return errors.New("must be an object")
		}
		for key, raw := range fingerprints {
			fingerprint, ok := raw.(string)
			if !ok {
				return errCanonicalPayloadRequired
			}
			if !validAzureTagMarker(fingerprint) {
				return fmt.Errorf("invalid marker for tag %q", key)
			}
		}
	default:
		return errCanonicalPayloadRequired
	}
	return nil
}

func validAzureTagMarker(value string) bool {
	const prefix = "redacted:hmac-sha256:"
	if len(value) != len(prefix)+64 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for i := len(prefix); i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func rejectSensitivePayloadKey(key, path string) error {
	if _, safe := redactionSafePayloadKeys[key]; !safe && sensitiveQueryPattern.MatchString(key) {
		return fmt.Errorf("sensitive-looking key %q must be redacted before emission", path)
	}
	return nil
}

func payloadKeyPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
