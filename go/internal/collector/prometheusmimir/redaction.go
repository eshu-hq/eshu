// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

func fingerprint(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fingerprintJoined(values ...string) string {
	return fingerprint(strings.Join(values, "\x00"))
}

func sortedKeys(values map[string]any) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys
}

func setStringSlice(payload map[string]any, key string, values []string) {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	sort.Strings(clean)
	if len(clean) > 0 {
		payload[key] = clean
	}
}

func setURLRedaction(payload map[string]any, field string, value string, redacted bool) {
	if strings.TrimSpace(value) == "" && !redacted {
		return
	}
	payload[field+"_present"] = strings.TrimSpace(value) != ""
	if fp := fingerprint(value); fp != "" {
		payload[field+"_fingerprint"] = fp
	}
	payload[field+"_redacted"] = true
}

func setRedactionState(payload map[string]any) {
	for key, value := range payload {
		if strings.HasSuffix(key, "_redacted") && value == true {
			payload["redaction_state"] = "applied"
			return
		}
		if strings.HasSuffix(key, "_fingerprint") {
			payload["redaction_state"] = "applied"
			return
		}
	}
	payload["redaction_state"] = "none"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
