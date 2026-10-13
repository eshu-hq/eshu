// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"crypto/sha256"
	"encoding/hex"
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

func setFingerprint(payload map[string]any, field string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		payload[field+"_present"] = true
		payload[field+"_fingerprint"] = fingerprint(value)
	}
}

func setURLRedaction(payload map[string]any, value string, redacted bool) {
	if strings.TrimSpace(value) == "" && !redacted {
		return
	}
	payload["url_present"] = strings.TrimSpace(value) != ""
	if fp := fingerprint(value); fp != "" {
		payload["url_fingerprint"] = fp
	}
	payload["url_redacted"] = true
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
