// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// The supply-chain impact writer persists explicit empty collections rather
// than JSON nulls, so API and MCP callers can range over a finding payload's
// collection fields without a nil guard. These helpers are the single place
// that rule is applied; keep new collection-shaped payload fields going
// through one of them rather than open-coding the nil check at the call site.

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilMapSlice(values []map[string]any) []map[string]any {
	if values == nil {
		return []map[string]any{}
	}
	return values
}

// nonNilStringMap is the map-shaped counterpart used by environment_evidence
// (issue #5426). That field is optional in the payload schema and carries
// omitempty, so an empty map is omitted from the wire either way — this
// normalizes the Go-side value so a consumer decoding the typed payload struct
// gets a rangeable map rather than a nil one.
func nonNilStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}
