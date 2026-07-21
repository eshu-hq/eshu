// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfconfigstate carries the helper Go for the Terraform
// config-vs-state drift correlation pack. The package implements the five
// drift classifiers, the attribute allowlist, and the cross-scope candidate
// builder consumed by the reducer handler before
// engine.Evaluate(rules.TerraformConfigStateDriftRulePack(), ...) is called.
//
// Current proof gates are documented in docs/public/reference/local-testing.md
// under "Terraform Config-vs-State Drift Compose Proofs". Tracking issue: #43
// (epic #50).
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
// (attribute_allowlist.go). Promotion to a versioned data file requires a new
// current design note before implementation.
//
// # Outcome model (issue #5442)
//
// Making drift durable (go/internal/reducer/terraform_config_state_drift_
// writer.go) required deciding which of the six-outcome vocabulary defined at
// docs/internal/design/391-observability-coverage-correlation.md:294-303
// (exact, derived, ambiguous, unresolved, stale, rejected) this domain
// actually reaches. DriftKind answers "what disagreed"; outcome answers "how
// confident is the join" -- they are orthogonal, and every durable finding
// carries both (DriftKind empty for an ambiguous-owner row, since no
// per-address classification ran).
//
// Reachable today:
//
//   - "exact" — every per-address finding this package's Classify/
//     BuildCandidates produce. The config<->state join is address-string
//     equality (see ResourceRow.Address's doc comment: "The classifier never
//     compares addresses internally; the candidate builder already keys
//     candidates on this value"), and attribute comparison
//     (classifyAttributeDrift) is a direct concrete-value comparison, not a
//     derived or heuristic match. There is exactly one classification path,
//     so every emitted per-address finding is exact by construction.
//   - "ambiguous" — NOT a per-address outcome. Genuine ambiguity in this
//     domain lives one level up, at backend-owner resolution
//     (tfstatebackend.ResolveConfigCommitForBackend): when more than one
//     distinct repo claims the same (backend_kind, locator_hash), the
//     handler has no single anchor to join addresses against at all, so no
//     per-address candidate is ever built. Following the design doc's
//     ambiguous precedent ("record candidate identities, do NOT pick one"),
//     the writer persists ONE durable finding for the whole rejected state
//     scope (Address and DriftKind empty, AmbiguousOwnerCandidates carrying
//     every competing repo's identity) rather than either silently dropping
//     the rejection to a log line or stamping a guessed winner exact.
//
// Deliberately not reachable / not persisted:
//
//   - "unresolved" (tfstatebackend.ErrNoConfigRepoOwnsBackend, zero candidate
//     repos) stays log-only, not a durable finding. Unlike the ambiguous
//     case, there is no competing evidence to record — "no Eshu-tracked repo
//     owns this backend" is not actionable drift evidence, and persisting one
//     row per untracked backend per generation would be unbounded write
//     volume for zero operator value.
//   - "stale" is unreachable with the evidence this handler has today.
//     CommitAnchor records which single config commit currently owns the
//     backend (tfstatebackend.CommitAnchor.CommitObservedAt), but nothing in
//     the handler compares that timestamp against a staleness policy for the
//     state generation, and no such threshold exists elsewhere in the
//     codebase to borrow. Inventing one here would be an unmeasured,
//     arbitrary policy choice, not an honest outcome.
//   - "derived" is unreachable: nothing in this domain's join is a multi-hop
//     or heuristic inference the way AWS runtime drift's management_status
//     is (derived by combining multiple evidence classes). Every reachable
//     signal here is exact instead.
//   - "rejected" is not adopted as an outcome value; the existing
//     DriftRejection.FailureClass vocabulary (scope_not_state_snapshot,
//     resolver_unavailable, evidence_loader_unavailable,
//     no_config_repo_owns_backend, ambiguous_backend_owner) already covers
//     structural/operational rejection at the intent level and is orthogonal
//     to the per-finding outcome this package classifies.
package tfconfigstate
