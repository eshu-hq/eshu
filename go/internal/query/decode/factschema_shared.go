// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package decode

// DefaultSchemaMajorVersion is the schema version a query-layer decoder
// assumes when a row carries none. It is a major-1 version because every
// migrated supply-chain source-fact kind that uses it is at schema major 1
// today; the factschema Decode seam dispatches on the major component only.
const DefaultSchemaMajorVersion = "1.0.0"

// DerefString returns the value a *string points at, or "" when it is nil,
// matching the pre-typing StringVal("") behavior for a field a factschema
// migration converts from a raw payload lookup to a typed pointer.
func DerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
