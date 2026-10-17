# 7585 PR2 no-regression note: live-signal report query (Refs #7585)

Status: **no-regression by construction; no live measurement claimed.**

## No-Regression Evidence:

- Baseline: PR1 state (report vocabulary in
  `generation_retention_expired_blocked.go`, no store reader). After: PR2
  adds `SelectExpiredGenerations` on `GenerationRetentionStore`, a single
  lock-free `SELECT` evaluated through the PR1
  `EvaluateGenerationRetention`. No production caller invokes it yet
  (verified: only its definition and tests reference it), so no new
  statement executes on any runtime path. Runtime cost delta is zero.
- Shape safety for the future caller: the report mirrors the prune
  candidate selection's guards term for term (superseded-only,
  `superseded_at IS NOT NULL`, same strict ceiling predicate, same
  active-generation exclusion, same uncovered/delta-protected
  definitions); the two `fact_work_items` EXISTS probes follow the
  `(scope_id, generation_id, status)` index prefix and the report LIMIT
  bounds the probe count. One shared `now` binds the SQL cutoff to the
  evaluated expiry, so the report and the deletion bound cannot disagree
  at the ceiling edge.
- Proof: focused retention suite
  (`TestEvaluateGenerationRetention*`, `TestSelectExpiredGenerations*`)
  green in `go/internal/storage/postgres`; `go vet` clean. Live-store
  plan and row/WAL cost at production shape are NOT_RUN (no local
  Postgres on this host); the CI live lanes own that proof before merge.

## No-Observability-Change:

PR2 adds no metric, span, log line, or status field. Ceiling telemetry
arrives in PR3.
