// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

// correlations.go decodes typed *string/*bool pointer fields
// off the reducer package correlation structs
// (sdk/go/factschema/reducerderived/v1) and needs nil-safe deref semantics.
//
// Root package query has the same helper as derefString in
// factschema_decode_shared.go (named workItemDerefString there before #6642
// destuttered it; the workItemDerefBool twin was dropped in the same move,
// since no root caller needed it any more). derefString stays there: its
// only remaining root caller is factschema_decode_supplychain.go --
// work_item_evidence.go moved to internal/query/workitem/evidence.go and
// calls its own package-local derefString now, so it no longer needs root's
// -- so #6060's family move cannot take it, and an unexported root symbol
// cannot be called across a package boundary. Root exports no equivalent to
// wrap, so this family carries its own copy of the same trivial logic rather
// than a forwarder.
//
// They are named for what they do here rather than for the root file they came
// from: nothing in this package is work-item-shaped, and carrying that prefix
// across would misdescribe them and invite the next reader to copy them
// somewhere under the wrong semantics (#6060 review).

// derefString returns the value a *string points at, or "" when it is nil.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// derefBool returns the value a *bool points at, or false when it is nil.
func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
