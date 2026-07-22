// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command golden-corpus-gate is the typed assertion step of the B-7 golden
// end-to-end corpus gate (issue #3800).
//
// It diffs a live pipeline run against the B-12 golden snapshot
// (testdata/golden/e2e-20repo-snapshot.json) and asserts the B-7 acceptance
// buckets:
//
//   - drains: fact_work_items residual rows and shared_projection_intents
//     nonterminal rows both reach their snapshot bound. The
//     shared_projection_intents check is the B-13 (#3859) gate — a zero
//     fact_work_items queue alone misses held projection intents, so this gate
//     also waits for the projection-intent ledger to reach a terminal state.
//   - graph: required correlations (rc-1 deployable-unit, rc-3 DEPENDS_ON, ...)
//     must exist, and required edge/node properties must be present — e.g. the
//     source_tool provenance token on Tier-2 shared-verb edges and a non-empty
//     language on File nodes (#3997) — so a provenance regression fails the gate
//     instead of passing silently. Per-label node and per-relationship edge
//     counts are asserted as required against the snapshot tolerances in the full
//     20-repo mode (-graph-required-only=false, #3866); the ranges are calibrated
//     to the real deterministic corpus output, not aspirational values.
//   - query: canonical HTTP responses carry their required shape.
//     CODEOWNERS HTTP and MCP assertions use the fixture's deterministic
//     canonical Repository.id rather than its display name; SQL relationship
//     assertions are backed by the explicitly staged sql_comprehensive fixture.
//   - demo-answers: the five specs/demo-first-answers.v1.yaml questions are
//     executed live with their SPECIFIC pinned arguments (via
//     go/internal/demospec) and each must return a populated answer. The query
//     phase asserts each tool/route with the snapshot's generic example
//     arguments; this phase guards the demo oracle's own arguments (#4776) so a
//     first-run demo answer cannot silently regress to empty.
//   - timing: total pipeline wall time stays within a budget multiplier, and —
//     when -phase-timings-file is given (B-11, #3804) — each gated phase stays
//     within its testdata/golden/e2e-baseline.json baseline (band OR absolute
//     slack). The per-phase check is advisory on shared CI runners
//     (-phase-regression-advisory) and blocking on a controlled host.
//
// The command connects to a Postgres DSN, a graph backend, and a running
// eshu-api using the same environment variables the services under test use
// (ESHU_POSTGRES_DSN, ESHU_GRAPH_BACKEND, NEO4J_URI, ESHU_API_KEY, ...). The
// orchestration that actually runs the pipeline — bootstrap-index over a
// minimal repo corpus, the B-10 cassette collectors, and the reducer drain —
// lives in scripts/verify-golden-corpus-gate.sh, which invokes this command for
// each phase.
//
// Exit status is non-zero when any required finding fails; advisory findings are
// printed but never fail the gate. An empty report (no assertions executed) is
// treated as a failure.
package main
