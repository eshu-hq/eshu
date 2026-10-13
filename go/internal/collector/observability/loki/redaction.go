// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

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

func sortedKeys(values map[string]string) []string {
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

func sortedAnyKeys(values map[string]any) []string {
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

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	sort.Strings(out)
	return out
}

func setStringSlice(payload map[string]any, key string, values []string) {
	clean := cleanStringSlice(values)
	if len(clean) > 0 {
		payload[key] = clean
	}
}

func setIntMap(payload map[string]any, key string, values map[string]int) {
	if len(values) == 0 {
		return
	}
	clean := map[string]int{}
	for label, count := range values {
		if trimmed := strings.TrimSpace(label); trimmed != "" && count >= 0 {
			clean[trimmed] = count
		}
	}
	if len(clean) > 0 {
		payload[key] = clean
	}
}

func setStringSliceMap(payload map[string]any, key string, values map[string][]string) {
	if len(values) == 0 {
		return
	}
	clean := map[string][]string{}
	for label, hashes := range values {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}
		cleanHashes := cleanStringSlice(hashes)
		if len(cleanHashes) > 0 {
			clean[trimmed] = cleanHashes
		}
	}
	if len(clean) > 0 {
		payload[key] = clean
	}
}

func setRedactionState(payload map[string]any) {
	for key, value := range payload {
		if strings.HasSuffix(key, "_redacted") && value == true {
			payload["redaction_state"] = "applied"
			return
		}
		if strings.HasSuffix(key, "_fingerprint") || strings.HasSuffix(key, "_hashes") {
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
