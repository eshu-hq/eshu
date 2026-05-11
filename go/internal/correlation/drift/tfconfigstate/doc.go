// Package tfconfigstate carries the helper Go for the Terraform
// config-vs-state drift correlation pack. The package implements the five
// drift classifiers, the attribute allowlist, and the cross-scope candidate
// builder consumed by the reducer handler before
// engine.Evaluate(rules.TerraformConfigStateDriftRulePack(), ...) is called.
//
// Design contract:
//
//	docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md
//
// Tracking issue: #43 (epic #50).
//
// The package lives under go/internal/correlation/drift/ rather than under
// go/internal/correlation/rules/ to avoid a circular import: rules carries
// the rule-pack declaration consumed here, and the classifier helpers must
// in turn reference the rule-pack name constant when stamping
// Candidate.Kind. Keeping the classifier in a sibling tree means rules stays
// pure metadata and drift comparison stays out of the rule-pack declaration.
//
// Exported surface:
//
//   - DriftKind, AllDriftKinds, DriftKind.Validate — the closed enum that
//     labels eshu_dp_correlation_drift_detected_total{drift_kind}.
//   - ResourceRow — the normalized config / state / prior view of one
//     Terraform resource address fed to the classifier.
//   - Classify — the top-level dispatcher. Returns the matching DriftKind
//     or empty when no drift fires.
//   - AddressedRow — the joined per-address input to the candidate builder.
//   - BuildCandidates — emits one correlation Candidate per drifted address;
//     carries cross-scope EvidenceAtoms tying the config and state ScopeIDs.
//   - AllowlistFor, AllowlistResourceTypes — the per-resource-type ordered
//     attribute allowlist consulted by classifyAttributeDrift.
//   - Evidence type and key constants — stable tokens read by the rule
//     pack's structural admission gate and the explain trace.
//
// Invariants:
//
//   - Computed/unknown config-side attribute values never raise
//     attribute_drift against a concrete state value. The classifier
//     consults ResourceRow.UnknownAttributes before comparing values.
//   - LineageRotation on a prior state row suppresses removed_from_state.
//   - PreviouslyDeclaredInConfig is required for removed_from_config to
//     fire; raw state-only presence is not enough.
//   - BuildCandidates emits candidates in address-sorted order so explain
//     traces are deterministic across reducer reruns.
//
// Attribute allowlist ownership note: the v1 allowlist is compile-time
// (attribute_allowlist.go). Promotion to a versioned data file is a
// follow-up tracked in design doc §9 question Q5.
package tfconfigstate
