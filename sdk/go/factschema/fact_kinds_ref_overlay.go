// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

// The ref_overlay family has one DOTTED kind (ref_overlay.branch_observation,
// matching the incident/service_catalog/submodule convention) and one BARE
// kind (config_key_divergence, the exact wire string issue #5393 names).
// The values MUST stay byte-equal to facts.RefOverlayBranchObservationFactKind
// and facts.ConfigKeyDivergenceFactKind (go/internal/facts/ref_overlay.go);
// the contracts module cannot import go/internal/facts, so the strings are
// duplicated here, per Contract System v1 §3.1. The reducer-side
// TestFactSchemaKindsMatchWireFactKinds drift lock asserts the equality.

// FactKindRefOverlayBranchObservation is the "ref_overlay.branch_observation"
// fact kind: one deployment-referenced non-default ref.
const FactKindRefOverlayBranchObservation = "ref_overlay.branch_observation"

// FactKindConfigKeyDivergence is the "config_key_divergence" fact kind: one
// per-environment config-key divergence row, keys plus a boolean only.
const FactKindConfigKeyDivergence = "config_key_divergence"
