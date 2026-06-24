// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func referencePayload(ref Reference) map[string]string {
	payload := map[string]string{}
	setStringMap(payload, "id", ref.ID)
	setStringMap(payload, "type", ref.Type)
	setStringMap(payload, "summary", ref.Summary)
	setStringMap(payload, "url", safeSourceURI(ref.HTMLURL))
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func referencesPayload(refs []Reference) []map[string]string {
	out := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		if payload := referencePayload(ref); len(payload) > 0 {
			out = append(out, payload)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func linksPayload(links []Link) []map[string]string {
	out := make([]map[string]string, 0, len(links))
	for _, link := range links {
		href := safeSourceURI(link.Href)
		text := strings.TrimSpace(link.Text)
		if href == "" && text == "" {
			continue
		}
		out = append(out, map[string]string{"href": href, "text": text})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func setString(payload map[string]any, key string, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		payload[key] = trimmed
	}
}

func setStringMap(payload map[string]string, key string, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		payload[key] = trimmed
	}
}

func setReferenceID(payload map[string]any, key string, ref Reference) {
	setString(payload, key, ref.ID)
}

func setReferenceIDs(payload map[string]any, key string, refs []Reference) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if value := strings.TrimSpace(ref.ID); value != "" {
			out = append(out, value)
		}
	}
	if len(out) > 0 {
		payload[key] = out
	}
}

func setTimePayload(payload map[string]any, key string, value time.Time) {
	if !value.IsZero() {
		payload[key] = value.UTC().Format(time.RFC3339Nano)
	}
}

func setConfigBooleans(payload map[string]any, disabled, deleted, manuallyCreated bool) {
	if disabled {
		payload["disabled"] = true
	}
	if deleted {
		payload["deleted"] = true
	}
	if manuallyCreated {
		payload["manually_created"] = true
	}
}

func setSensitiveFingerprint(payload map[string]any, key string, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		sum := sha256.Sum256([]byte(trimmed))
		payload[key] = "sha256:" + hex.EncodeToString(sum[:])
	}
}

func setRedactionState(payload map[string]any) {
	if _, ok := payload["name_fingerprint"]; ok {
		payload["redaction_state"] = "fingerprinted"
		return
	}
	payload["redaction_state"] = "none"
}

func providerStableKey(kind string, scopeID string, providerID string) string {
	return stableID(kind, map[string]any{
		"provider":    SourceSystem,
		"scope_id":    strings.TrimSpace(scopeID),
		"provider_id": strings.TrimSpace(providerID),
	})
}

func stableID(factType string, identity map[string]any) string {
	raw, err := json.Marshal(map[string]any{"fact_type": factType, "identity": identity})
	if err != nil {
		panic(fmt.Sprintf("marshal stable id payload: %v", err))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func safeSourceURI(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func sensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"integration_key", "key", "password", "passwd", "routing_key",
		"secret", "sig", "signature", "token":
		return true
	default:
		return false
	}
}

func timeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
