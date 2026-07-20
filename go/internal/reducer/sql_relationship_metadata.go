// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// sqlMetadataString extracts a string value from SQL entity metadata.
func sqlMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// sqlMetadataStringSlice extracts a string slice from SQL entity metadata.
func sqlMetadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return nil
	}
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok || s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

// sqlMetadataMapSlice extracts a slice of maps from SQL entity metadata. It
// accepts both the in-process []map[string]any shape (direct Go construction,
// unit tests) and the []any-of-map[string]any shape a JSON-decoded facts
// envelope produces, mirroring sqlMetadataStringSlice's dual-shape handling.
// Used for migration_targets (#5346).
func sqlMetadataMapSlice(metadata map[string]any, key string) []map[string]any {
	if metadata == nil {
		return nil
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return nil
	}
	switch typed := v.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, m)
		}
		return out
	default:
		return nil
	}
}
