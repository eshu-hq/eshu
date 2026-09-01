// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

// package_registry_correlations.go decodes typed *string/*bool pointer fields
// off the reducer package correlation structs
// (sdk/go/factschema/reducerderived/v1) and needs nil-safe deref semantics.
//
// Root package query has the same two helpers as workItemDerefString and
// workItemDerefBool in factschema_decode_workitem.go. They stay there: many
// root decode files call them (work_item_evidence.go, incident_context_*.go,
// supply_chain_advisory_evidence_model.go, factschema_decode_supplychain.go),
// so #6060's family move cannot take them, and an unexported root symbol
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
