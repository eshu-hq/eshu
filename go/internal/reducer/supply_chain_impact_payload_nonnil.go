// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// The supply-chain impact writer persists explicit empty collections rather
// than JSON nulls, so API and MCP callers can range over a finding payload's
// collection fields without a nil guard. These helpers are the single place
// that rule is applied; keep new collection-shaped payload fields going
// through one of them rather than open-coding the nil check at the call site.
//
// The exception is an OPTIONAL (omitempty) payload field, which must stay nil
// when empty so the encoder can drop the key entirely. environment_evidence
// (#5426) is one: wrapping it here would emit an empty object and break the
// "omitted when absent" contract the HTTP API reference states.

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
