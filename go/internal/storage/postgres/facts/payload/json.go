// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadstore

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalPayload encodes payload as Postgres-JSONB-safe JSON. An empty or nil
// payload marshals to the empty object "{}" rather than "null".
func MarshalPayload(payload map[string]any) ([]byte, error) {
	if len(payload) == 0 {
		return []byte("{}"), nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	data = sanitizeJSONB(data)
	if !json.Valid(data) {
		return []byte("{}"), nil
	}

	return data, nil
}

// sanitizeJSONB cleans marshaled JSON bytes for Postgres JSONB compatibility.
//
// Postgres JSONB rejects \u0000 escapes and raw control bytes. Source payloads
// may contain these bytes when repositories include binary or non-UTF-8 content.
func sanitizeJSONB(data []byte) []byte {
	needsSanitize := false
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			needsSanitize = true
			break
		}
	}
	if !needsSanitize && !bytes.Contains(data, []byte(`\u0000`)) {
		return data
	}

	data = stripUnescapedJSONNulls(data)

	cleaned := make([]byte, 0, len(data))
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			continue
		}
		cleaned = append(cleaned, b)
	}

	return cleaned
}

// stripUnescapedJSONNulls removes JSON null escapes while preserving source
// text that contains the six literal characters \u0000. JSON encodes that
// literal text with an escaped backslash (\\u0000); removing the suffix from
// the escaped form corrupts the JSON and can discard the entire fact payload.
func stripUnescapedJSONNulls(data []byte) []byte {
	const nullEscape = `\u0000`

	var cleaned []byte
	copyStart := 0
	for i := 0; i+len(nullEscape) <= len(data); i++ {
		if !bytes.Equal(data[i:i+len(nullEscape)], []byte(nullEscape)) {
			continue
		}

		precedingBackslashes := 0
		for j := i - 1; j >= 0 && data[j] == '\\'; j-- {
			precedingBackslashes++
		}
		if precedingBackslashes%2 != 0 {
			continue
		}

		if cleaned == nil {
			cleaned = make([]byte, 0, len(data)-len(nullEscape))
		}
		cleaned = append(cleaned, data[copyStart:i]...)
		copyStart = i + len(nullEscape)
		i = copyStart - 1
	}
	if cleaned == nil {
		return data
	}
	return append(cleaned, data[copyStart:]...)
}

// UnmarshalPayload decodes a JSONB payload column into a map. Empty input, or
// a decoded empty object, returns a nil map with no error.
func UnmarshalPayload(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode payload json: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}

	return payload, nil
}

// EmptyToNil returns nil for an empty string, or value otherwise. Callers use
// it to bind optional string columns as SQL NULL instead of "".
func EmptyToNil(value string) any {
	if value == "" {
		return nil
	}

	return value
}

// EmptyToDefault returns fallback when value is empty, or value otherwise.
func EmptyToDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
