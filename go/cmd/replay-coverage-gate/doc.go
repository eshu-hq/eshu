// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command replay-coverage-gate is the C-1 replay coverage manifest + lockstep
// gate (issue #4173, epic #4172). It enumerates every surface Eshu claims to
// support from the four source-of-truth registries and reports any supported
// surface lacking a green replay scenario.
//
// The four registries it reconciles:
//
//   - surface-inventory: collectors on the implemented readiness lane (only that
//     lane asserts production readiness) — required cassette scenarios.
//   - fact-kind registry: each distinct read_surface — required API/MCP golden
//     scenarios.
//   - parser-backing ledger: each parser — required parser-fixture scenarios.
//   - capability matrix: each positively-claimed capability — required
//     claim-or-refusal scenarios.
//
// Each supported surface is reconciled against the curated coverage manifest
// (specs/replay-coverage-manifest.v1.yaml), which maps a surface to the scenario
// that exercises it. A surface with no mapping is uncovered; a mapping whose
// artifact is missing is unresolved; a mapping for a surface no registry claims
// is stale drift.
//
// Local runs are advisory by default; the single -blocking flag flips every
// uncovered, unresolved, and stale finding to required. CI passes -blocking now
// that C-2..C-6 have burned the gaps down, so coverage can never regress. When
// requested, the command writes a machine-readable coverage report (-report-out)
// and the committed, docs-discoverable C-7 dashboard (-dashboard-out,
// docs/public/reference/replay-coverage.md);
// TestCommittedDashboardIsCurrent holds the dashboard in lockstep (refresh with
// -update-dashboard).
//
// Coverage is existence-only: the gate proves a scenario artifact exists and is
// wired, not that it passes. Greenness is proven by the sibling gate named in
// each manifest entry's proof_gate (golden-corpus-gate, the parser fixture
// tests). That keeps this gate fast, credential-free, and Docker-free.
//
// Usage:
//
//	replay-coverage-gate \
//	  -specs-dir specs \
//	  -snapshot testdata/golden/e2e-20repo-snapshot.json \
//	  -repo-root . \
//	  -report-out coverage-report.json
//
// Add -blocking to make a coverage regression fail the run.
package main
