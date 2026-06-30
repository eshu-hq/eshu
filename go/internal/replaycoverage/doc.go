// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package replaycoverage reconciles the surfaces Eshu claims to support, and the
// scenario-depth classes each surface requires, against the replay scenarios that
// exercise them. It produces the C-1/C-8/C-9/C-10/C-13 coverage-manifest lockstep
// gate (issues #4173, #4187, #4188, #4189, and #4366, epic #4172).
//
// # The source registries
//
// Every supported surface is enumerated from an in-repo source of truth:
//
//   - surface-inventory (capabilitycatalog.SurfaceInventory): collectors on the
//     implemented readiness lane — only that lane asserts production readiness —
//     are required to have a cassette replay scenario.
//   - fact-kind registry (facts.FactKindRegistry): each distinct read_surface is
//     required to have an API/MCP golden replay scenario.
//   - B-12 CLI query shapes (goldengate.QueryShapes.CLI): each CLI read surface
//     is required to have a CLI golden replay scenario whose parity metadata is
//     asserted by the golden-corpus gate.
//   - parser-backing ledger (LoadParserLedger): each parser is required to have a
//     parser-fixture replay scenario.
//   - capability matrix (capabilitycatalog.Matrix): each positively-claimed
//     capability is required to have a capability-claim replay scenario whose
//     profile rows name supported-answer and refusal verification.
//   - product claim ledger (capabilitycatalog.ProductClaimLedger): each broad
//     public product claim is required to have a product-claim replay scenario
//     whose row carries deterministic proof.
//   - authorization catalog (capabilitycatalog.AuthorizationCatalog): each live
//     permission family is required to have in-grant and out-of-grant
//     scoped-route replay scenarios.
//
// EnumerateSupported flattens these into a deterministic SupportedSurface set,
// each with a canonical "<kind>:<name>" coverage key.
//
// # Depth requirements per applicable surface (C-13)
//
// Each surface is then expanded into the depth classes it requires: baseline,
// delta_tombstone, fault, ordering, crash, and cost. Before C-13 these came only
// from manifest scenario_requirements rows, so the manifest required just one
// scenario of each non-baseline type for the whole system — leaving a
// delete/crash/fault hole free to recur for any other piece while the gate read
// 100% (proven by #4186). DeriveRequirements now derives them PER applicable
// surface from the source registries:
//
//   - fault for every collector boundary (every implemented collector:* surface),
//   - cost for every projection (every distinct reducer_domain in the fact-kind
//     registry),
//   - ordering for every shared-conflict-key projection (a reducer_domain written
//     by two or more distinct projection hooks),
//   - delta_tombstone for every retractable graph node type, and
//   - crash for the reducer drain.
//
// The retractable node types and the reducer drain are declared in
// specs/replay-depth-requirements.v1.yaml (LoadDepthRequirements); a lockstep
// test keeps the node-type list byte-equal to cypher.RetractableNodeEntityLabels()
// so a new retractable label makes the gate demand a delta scenario instead of
// the gap going unseen. EnumerateDepthSurfaces enumerates the retractable_node,
// projection, and reducer_drain surfaces; the derived requirements are unioned
// with the manifest's explicit scenario_requirements before reconciliation.
//
// # The manifest and reconciliation
//
// The coverage manifest (specs/replay-coverage-manifest.v1.yaml, loaded by
// LoadManifest) is the curated declaration mapping each supported surface and
// scenario_type to the replay scenario that covers it, plus audited exemptions.
// Reconcile maps every required surface/scenario_type pair to a Status (covered,
// uncovered, unresolved, exempt) using the manifest and a Resolver that verifies
// the referenced scenario artifact actually exists. Manifest entries that map no
// supported requirement are reported as stale drift.
//
// Resolution is existence-only by design: this gate proves a scenario is authored
// and wired, not that it passes — its greenness is proven by the sibling gate
// named in the entry's proof_gate (golden-corpus-gate, replay tier, Go race
// tests, parser fixture tests, capability-inventory, capability-inventory-docs,
// authz-scoped-route-tests, or capability-budget proof). Capability-claim
// entries are resolved against the capability matrix and require profile
// verification references before they can count as covered. Product-claim
// entries are resolved against the public claim ledger and require deterministic
// proof metadata; capability-inventory docs mode validates the full
// quote/surface/proof contract. Authz-scoped-route entries resolve against the
// authorization replay proof ledger. That split keeps the coverage gate fast and
// credential-free while never claiming a green it did not observe.
//
// ValidateRequiredProofGates binds those proof_gate names back to the CI-gate
// registry. Unknown proof gates, proof gates with no local command, and proof
// gates with neither a CI workflow nor a local-only rationale are unresolved
// proof metadata, not covered replay scenarios.
//
// # The language-parser scoreboard (C-11)
//
// BuildLanguageScoreboard produces a separate, visibility-only scoreboard
// (issue #4364) over every language in the language-feature-parity ledger
// (LoadLanguageLedger), so no language Eshu claims to parse is silently absent
// from the coverage count. Each language is exempt when it is exercised
// end-to-end by the golden-corpus corpus (declared in the manifest's
// language_exemptions list) or uncovered — the C-12 (#4365) parser-fixture
// backfill worklist. The scoreboard is deliberately kept out of EnumerateSupported
// and Findings: tree-sitter languages can have a fixture, so they are honest
// uncovered gaps rather than exemptions, and listing those gaps must not fail the
// blocking gate. It is rendered into the coverage report and the C-7 dashboard
// only.
//
// # Advisory to blocking
//
// Findings reuse the shared goldengate.Finding/Report machinery. Local advisory
// mode (Blocking=false) reports every coverage gap without failing the command.
// CI passes the single blocking flag after the C-2..C-10 burn-down, so every
// uncovered, unresolved, and stale BASELINE (breadth) finding is required and
// breadth coverage cannot regress. The C-8/C-13 depth classes are advisory-first:
// isBlockingScenarioType keeps every non-baseline finding advisory even under
// -blocking, so the gate enumerates and reports the missing surface/scenario_type
// pairs (the C-14 #4367 backfill worklist) without failing CI, until C-14 burns
// them down and a later ticket flips depth to blocking. BuildReport emits the
// machine-readable coverage-report artifact, including per-scenario_type
// summaries and the language-parser scoreboard, that the C-7 dashboard consumes
// on every run.
package replaycoverage
