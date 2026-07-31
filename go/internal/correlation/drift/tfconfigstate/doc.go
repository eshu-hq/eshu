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
//     BuildCandidates produce. The COMPARISON step is exact: the
//     config<->state join is address-string equality (see
//     ResourceRow.Address's doc comment: "The classifier never compares
//     addresses internally; the candidate builder already keys candidates on
//     this value"), and attribute comparison (classifyAttributeDrift) is a
//     direct concrete-value comparison, not a derived or heuristic match.
//
//     This label is honest about the comparison, not about every upstream
//     input to it. The config-side ResourceRow.Address this package receives
//     is computed by the evidence loader
//     (go/internal/storage/postgres/tfstate_drift_evidence_module_prefix.go),
//     which resolves a module-nested resource's address prefix by walking
//     `module {}` call chains (buildModulePrefixMap /
//     modulePrefixForPath). That resolution has a documented heuristic step:
//     classifyModuleSource's Terraform-Registry-shorthand discriminator
//     ("namespace/name/provider", three slash-separated segments) is a
//     pattern match, not certain knowledge, and the function's own comment
//     names the false-positive case — a repo whose top-level directory is
//     literally "terraform-aws-modules" misclassifies a local module source
//     as an external registry reference. When a module call cannot be
//     resolved (this case, or external_git / external_archive /
//     cross_repo_local / cycle_detected / depth_exceeded),
//     modulePrefixForPath returns no prefix for that callee directory, and
//     every resource declared under it is addressed as if it were a
//     root-module resource — a config-side address that will not match the
//     real (module-prefixed) state address. The comparison step then
//     correctly reports what it was given: a spurious added_in_config for
//     the wrongly-addressed config resource and a spurious added_in_state for
//     the real state resource, both stamped "exact" because the string
//     comparison that produced them genuinely was exact.
//
//     No per-finding signal distinguishes this today: a
//     TerraformConfigStateDriftFinding row carries no marker for "this
//     address came from an unresolved module chain." The only operator-
//     visible mitigation is scope-level, not finding-level:
//     eshu_dp_drift_unresolved_module_calls_total{reason} increments once per
//     unresolved module call during the same evidence-load pass that built
//     the (possibly wrong) address, so an operator who sees a surprising
//     added_in_config/added_in_state pair for a resource inside a module
//     block should check that counter for the same scope/generation before
//     trusting the finding as genuine drift — it is a candidate explanation,
//     not a proven one, since the counter is not joined to the specific
//     finding.
//
//     Decision: "exact" stays the label for v1 rather than reopening the
//     outcome model, for three reasons. First, this matches how "exact" is
//     already used elsewhere in this vocabulary — AWS runtime drift's
//     "exact" outcome (managementStatusManagedByTerraform) also does not
//     guarantee its own upstream ARN/tag extraction is infallible; "exact"
//     has always meant "the join mechanism was direct," not "every upstream
//     input was independently verified." Second, the failure mode is
//     pre-existing in PostgresDriftEvidenceLoader and shared by all five
//     drift kinds, not introduced by durability (issue #5442) — durability
//     makes an existing risk persistent and queryable rather than creating a
//     new one. Third, a real fix requires threading a per-address
//     module-resolution-confidence signal from buildModulePrefixMap through
//     AddressedRow/ResourceRow into a new Candidate evidence atom and
//     deciding how it maps onto outcome (most likely "derived" for
//     module-nested resources whose containing chain had any unresolved
//     segment) — a genuine code and outcome-model change with its own
//     TDD/proof cycle, not a documentation fix, and larger than this issue
//     should carry. If a future finding shows this false-positive class is
//     material in practice (not just theoretically possible), that plumbing
//     is the concrete follow-up: thread module-resolution status onto
//     ResourceRow, have BuildCandidates read it, and split "exact" into
//     "exact" (unambiguous module chain or no module nesting) versus
//     "derived" (address depends on an unresolved module-prefix fallback).
//
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
//   - "unresolved" — also NOT a per-address outcome, one level up at
//     backend-owner resolution like "ambiguous," but for the opposite
//     cardinality: zero distinct repos claim the (backend_kind, locator_hash)
//     (tfstatebackend.ErrNoConfigRepoOwnsBackend), rather than more than one.
//     Superseded decision (issue #5594, overriding the #5442-era call
//     recorded in this package's history): this outcome WAS log-only through
//     #5442, on the reasoning that "no Eshu-tracked repo owns this backend"
//     is not actionable drift evidence and persisting one row per untracked
//     backend per generation is unbounded write volume for zero operator
//     value. #5594 revisited that trade-off and the repo owner chose
//     visibility: the writer now persists ONE durable finding for the whole
//     rejected state scope (Address and DriftKind empty,
//     AmbiguousOwnerCandidates also empty — there is no competing evidence to
//     record here, only the absence of any owner), the exact ambiguous
//     precedent extended to the zero-owner case. The "unbounded write volume"
//     concern is addressed, not ignored: this write grows the SAME already-
//     accepted rate the ambiguous write already has (one row per
//     scope+generation, upserted by stable fact id, and only the active
//     generation's row is ever queryable — see
//     go/internal/storage/postgres/terraform_config_state_drift_findings.go's
//     `scope.active_generation_id = fact.generation_id` filter), not a new
//     unbounded category. The Terragrunt local-backend case that originally
//     surfaced this gap (a `remote_state { backend = "local" }` block whose
//     path resolves into Terragrunt's ephemeral `.terragrunt-cache` working
//     directory, which a static parser cannot observe or default against) is
//     one instance of this same no_config_repo_owns_backend class, not a
//     special case — see go/internal/collector/terraformstate/source_terragrunt.go.
//
//     Post-deployment volume watch (recorded, not yet a defect): the
//     disproof above is about per-key write CADENCE — one row per
//     scope+generation, the same rate "ambiguous" has always had — and that
//     disproof holds regardless of scale. It does NOT bound the number of
//     DISTINCT scopes that land in "unresolved" at once. An unonboarded
//     repo's backend is the ordinary way to reach
//     no_config_repo_owns_backend (no Eshu-tracked repo declares it, full
//     stop), so in a partially-onboarded org the count of distinct
//     "unresolved" scopes could plausibly exceed the count of "ambiguous"
//     scopes by a wide margin — ambiguity requires two-or-more repos to
//     coincidentally claim the same backend, which is rare, while
//     "unresolved" only requires one backend Eshu has not indexed yet, which
//     is common during rollout. If a future report shows this materially
//     inflates the drift-findings read surface or its underlying fact
//     volume, the mitigation is scope-level (e.g. a distinct-scope gauge or
//     a query-time cap), not a change to this per-scope write cadence.
//
//     Golden-corpus proof: this write path, and the "backend \"local\" {}"
//     default-path fix itself, are both exercised end-to-end by the corpus.
//     The corpus's original Terraform-state cassette/fixture pair
//     (testdata/cassettes/terraformstate/supply-chain-demo.json,
//     tests/fixtures/ecosystems/terraform_comprehensive/main.tf) stays
//     deliberately backend-aligned by #5442's own design so that scope's
//     ownership resolution always succeeds. Issue #5594 added two more
//     scopes to the SAME cassette alongside it: a resolvable
//     tests/fixtures/ecosystems/terraform_local_backend_demo (a bare
//     `backend "local" {}` block, proving the default-path fix resolves an
//     owner and materializes a real outcome="exact" finding) and a
//     deliberately orphaned bucket/key no fixture's config declares (proving
//     outcome="unresolved" fires for real, not just in unit tests). Both are
//     asserted live at testdata/golden/e2e-20repo-snapshot.json's
//     'POST /api/v0/terraform/config-state-drift/findings?variant=...' HTTP
//     query shapes, distinguished from each other and from the pre-existing
//     S3 scope by an outcome filter plus required_json_values.
//
// Deliberately not reachable / not persisted:
//
//   - "stale" is unreachable with the evidence this handler has today.
//     CommitAnchor records which single config commit currently owns the
//     backend (tfstatebackend.CommitAnchor.CommitObservedAt), but nothing in
//     the handler compares that timestamp against a staleness policy for the
//     state generation, and no such threshold exists elsewhere in the
//     codebase to borrow. Inventing one here would be an unmeasured,
//     arbitrary policy choice, not an honest outcome.
//   - "derived" is not emitted today, but — see the "exact" caveat above —
//     it is not cleanly unreachable either. The comparison step itself is
//     never a multi-hop or heuristic inference the way AWS runtime drift's
//     management_status is (derived by combining multiple evidence
//     classes), but the config-side address that comparison consumes can
//     depend on a heuristic module-prefix resolution. This package does not
//     currently surface that distinction as a candidate/finding-level
//     signal, so every reachable per-address outcome is labeled "exact" as
//     documented above rather than split into "exact" vs "derived" by
//     module-resolution confidence.
//   - "rejected" is not adopted as an outcome value; the existing
//     DriftRejection.FailureClass vocabulary (scope_not_state_snapshot,
//     resolver_unavailable, evidence_loader_unavailable,
//     no_config_repo_owns_backend, ambiguous_backend_owner) already covers
//     structural/operational rejection at the intent level and is orthogonal
//     to the per-finding outcome this package classifies.
package tfconfigstate
