// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

// correlations.go decodes typed *string/*bool pointer fields
// off the reducer package correlation structs
// (sdk/go/factschema/reducerderived/v1) and needs nil-safe deref semantics.
//
// The *string case is decode.DerefString, shared by every query-layer
// factschema decoder since #6642 moved root's copy into internal/query/decode.
// derefBool stays here: no other query package needs a *bool deref.

// derefBool returns the value a *bool points at, or false when it is nil.
func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
