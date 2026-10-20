<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence: `empty_scope` collector emission has no hot-path cost (#8657)

The `empty_scope` emission touches three hot-listed files
(`snapshot_empty_scope.go`, `fact_builder.go`, `envelopes.go`), so this note
records the no-regression proof the perf-evidence gate requires. The writer
side rides peer PR #8679; this lane owns the collector emission only.

## No-Regression Evidence: empty-snapshot streaming before/after

A throwaway benchmark drove the production path
`buildStreamingGeneration` → `streamFacts` → drain for a walked-empty
snapshot (the exact path that now evaluates the predicate and the envelope
key). It ran on the base tip and on this head, back to back on the same
host, then was deleted from both trees.

- Baseline (`origin/main` `4b2466f3`): 85016 ns/op, 75849 ns/op
  (benchtime 2s, count 2, linux/arm64, go1.26.6, no backend: pure
  in-process).
- After (this head): 76561 ns/op, 74916 ns/op under identical settings.
- Input shape: empty `RepositorySnapshot` plus minimal test repository
  metadata; the run drains every emitted fact including the repository fact.

An earlier count-3 run read slower on the head (134–158 us/op vs 80–100
us/op) while unrelated lanes ran tests on the same host; the interleaved
rerun above collapses that gap to noise. The change adds about nine
length checks once per snapshot plus one map key only when declaring,
against an ~80 us path dominated by generation streaming. No regression.

## No-Observability-Change: existing signals cover the emission

The predicate and the envelope key emit no metric, span, or log of their
own. The snapshot stage stays covered by
`eshu_dp_collector_snapshot_stage_duration_seconds`, repository-level
completion by `eshu_dp_repo_snapshot_duration_seconds`, and the fact count
— including the repository fact — by `eshu_dp_facts_emitted_total`. The
coverage row names these signals. The `empty_scope` key appears only on
genuinely fileless facts; the envelope stable key is untouched, so
re-emission stays idempotent and no dashboard or alert changes.
