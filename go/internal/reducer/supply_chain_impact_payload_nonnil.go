// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"

// The supply-chain impact writer persists explicit empty collections rather
// than JSON nulls, so API and MCP callers can range over a finding payload's
// collection fields without a nil guard. These helpers are the single place
// that rule is applied; keep new collection-shaped payload fields going
// through one of them rather than open-coding the nil check at the call site.
//
// They apply to REQUIRED collection fields, which are always present on the
// wire and so must never be null. An optional (omitempty) field needs no
// wrapper: omitempty drops a zero-length map or slice whether it is nil or
// empty, so environment_evidence (#5426) is assigned directly and the helpers
// would be a no-op on it.

// nonNilStrings forwards to [payloadcore.NonNilStrings].
func nonNilStrings(values []string) []string {
	return payloadcore.NonNilStrings(values)
}

func nonNilMapSlice(values []map[string]any) []map[string]any {
	if values == nil {
		return []map[string]any{}
	}
	return values
}
