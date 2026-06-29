// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package replaycoverage reconciles the surfaces Eshu claims to support against
// the replay scenarios that exercise them, producing the C-1 coverage-manifest
// lockstep gate (issue #4173, epic #4172).
//
// # The four registries
//
// Every supported surface is enumerated from an in-repo source of truth:
//
//   - surface-inventory (capabilitycatalog.SurfaceInventory): collectors on the
//     implemented readiness lane — only that lane asserts production readiness —
//     are required to have a cassette replay scenario.
//   - fact-kind registry (facts.FactKindRegistry): each distinct read_surface is
//     required to have an API/MCP golden replay scenario.
//   - parser-backing ledger (LoadParserLedger): each parser is required to have a
//     parser-fixture replay scenario.
//   - capability matrix (capabilitycatalog.Matrix): each positively-claimed
//     capability is required to have a claim-or-refusal replay scenario.
//
// EnumerateSupported flattens these into a deterministic SupportedSurface set,
// each with a canonical "<kind>:<name>" coverage key.
//
// # The manifest and reconciliation
//
// The coverage manifest (specs/replay-coverage-manifest.v1.yaml, loaded by
// LoadManifest) is the curated declaration mapping each supported surface to the
// replay scenario that covers it, plus audited exemptions. Reconcile maps every
// supported surface to a Status (covered, uncovered, unresolved, exempt) using
// the manifest and a Resolver that verifies the referenced scenario artifact
// actually exists. Manifest entries that map no supported surface are reported as
// stale drift.
//
// Resolution is existence-only by design: this gate proves a scenario is authored
// and wired, not that it passes — its greenness is proven by the sibling gate
// named in the entry's proof_gate (golden-corpus-gate, the parser fixture tests).
// That split keeps the coverage gate fast and credential-free while never
// claiming a green it did not observe.
//
// # Advisory to blocking
//
// Findings reuse the shared goldengate.Finding/Report machinery. The gate ships
// advisory (Blocking=false): every coverage gap is reported but never fails CI,
// so its red output is the C-2..C-6 worklist. A single blocking flag flips every
// uncovered, unresolved, and stale finding to required so coverage can never
// regress once the gaps are burned down. BuildReport emits the machine-readable
// coverage-report artifact the C-7 dashboard consumes on every run.
package replaycoverage
