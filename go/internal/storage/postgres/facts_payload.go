package postgres

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func marshalPayload(payload map[string]any) ([]byte, error) {
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

	data = bytes.ReplaceAll(data, []byte(`\u0000`), nil)

	cleaned := make([]byte, 0, len(data))
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			continue
		}
		cleaned = append(cleaned, b)
	}

	return cleaned
}

func unmarshalPayload(raw []byte) (map[string]any, error) {
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

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func emptyToDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
