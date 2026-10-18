// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	refOverlayV1 "github.com/eshu-hq/eshu/sdk/go/factschema/ref_overlay/v1"
)

// DecodeRefOverlayBranchObservation decodes env.Payload into the latest
// refOverlayV1.BranchObservation struct for the
// "ref_overlay.branch_observation" fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. Callers receive
// either the decoded struct or a classified *DecodeError; they must never
// substitute a zero-value struct on error. Issue #5393 Phase 1: no reducer
// or query handler calls this yet — it exists so the checked-in schema and
// fixture pack are honest against a real decode path ahead of the consumers
// landing in sibling Phase 1 leaves.
func DecodeRefOverlayBranchObservation(env Envelope) (refOverlayV1.BranchObservation, error) {
	return decodeLatestMajor[refOverlayV1.BranchObservation](FactKindRefOverlayBranchObservation, env)
}

// EncodeRefOverlayBranchObservation marshals a
// refOverlayV1.BranchObservation into the map[string]any payload shape an
// Envelope carries. It is the inverse of DecodeRefOverlayBranchObservation
// for schema-version-1 payloads, used by this module's own round-trip tests.
func EncodeRefOverlayBranchObservation(observation refOverlayV1.BranchObservation) (map[string]any, error) {
	return encodeToPayload(observation)
}

// DecodeConfigKeyDivergence decodes env.Payload into the latest
// refOverlayV1.ConfigKeyDivergence struct for the "config_key_divergence"
// fact kind, dispatching on env.SchemaVersion major per Contract System v1
// §3.2. Callers receive either the decoded struct or a classified
// *DecodeError; they must never substitute a zero-value struct on error.
// Issue #5393 Phase 1: no reducer or query handler calls this yet — it
// exists so the checked-in schema and fixture pack are honest against a
// real decode path ahead of the consumers landing in sibling Phase 1 leaves.
func DecodeConfigKeyDivergence(env Envelope) (refOverlayV1.ConfigKeyDivergence, error) {
	return decodeLatestMajor[refOverlayV1.ConfigKeyDivergence](FactKindConfigKeyDivergence, env)
}

// EncodeConfigKeyDivergence marshals a refOverlayV1.ConfigKeyDivergence into
// the map[string]any payload shape an Envelope carries. It is the inverse
// of DecodeConfigKeyDivergence for schema-version-1 payloads, used by this
// module's own round-trip tests.
func EncodeConfigKeyDivergence(divergence refOverlayV1.ConfigKeyDivergence) (map[string]any, error) {
	return encodeToPayload(divergence)
}
