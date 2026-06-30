// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package replaycoverage reconciles the surfaces Eshu claims to support, and the
// scenario-depth classes each surface requires, against the replay scenarios that
// exercise them. It produces the C-1/C-8/C-9/C-10 coverage-manifest lockstep
// gate (issues #4173, #4187, #4188, and #4189, epic #4172).
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
// each with a canonical "<kind>:<name>" coverage key. Manifest
// scenario_requirements then expand each supported surface into one or more
// required depth classes: baseline, delta_tombstone, fault, ordering, crash, and
// cost. Surfaces without an explicit requirement row require baseline only;
// explicit requirement rows must still include baseline.
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
// # Advisory to blocking
//
// Findings reuse the shared goldengate.Finding/Report machinery. Local advisory
// mode (Blocking=false) reports every coverage gap without failing the command.
// CI now passes the single blocking flag after the C-2..C-10 burn-down, so every
// uncovered, unresolved, and stale finding is required and coverage cannot
// regress. BuildReport emits the machine-readable coverage-report artifact,
// including per-scenario_type summaries, that the C-7 dashboard consumes on
// every run.
package replaycoverage
