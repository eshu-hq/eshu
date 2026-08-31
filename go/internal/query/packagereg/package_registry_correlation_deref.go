// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

// package_registry_correlations.go decodes typed *string/*bool pointer fields
// off the reducer package correlation structs (sdk/go/factschema/reducerderived/v1)
// and needs the same nil-safe deref semantics as root package query's
// workItemDerefString/workItemDerefBool (factschema_decode_workitem.go). Those
// two helpers are NOT moved here: they are shared across many root decode
// files (work_item_evidence.go, incident_context_*.go,
// supply_chain_advisory_evidence_model.go, factschema_decode_supplychain.go),
// so #6060's family-move can only leave them in root, and an unexported root
// symbol cannot be called across the package boundary. This file carries this
// family's own copy of the same trivial logic rather than a forwarder, since
// root has no exported equivalent to wrap.

// workItemDerefString returns the value a *string points at, or "" when it is
// nil. Mirrors root package query's workItemDerefString.
func workItemDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// workItemDerefBool returns the value a *bool points at, or false when it is
// nil. Mirrors root package query's workItemDerefBool.
func workItemDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
