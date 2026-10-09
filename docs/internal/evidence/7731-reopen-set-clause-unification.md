# Reopen SET-Clause Unification Evidence

Issue #7731. The admin reopen UPDATE (`reopenReducerWorkQuery`,
`go/internal/query/admin/store/reopen.go`) and the two replay reopen
queries (`reopenSucceededReducerWorkQuery`,
`replaySucceededReducerDomainQuery`,
`go/internal/storage/postgres/reducer_queue_replay.go`) each carried
their own copy of the succeeded-to-pending SET list. The PR introduces
one exported `ReopenSucceededReducerSetClause` const that all three
queries compose, plus `TestReopenSucceededReducerSetClauseCoversClaimColumns`,
which pins the clause to every state column the reducer claim path reads.

## No-Regression Evidence (#7731):

- Baseline: the three statements as emitted on base `01ceb1dd05ca`,
  extracted verbatim (`git show <base>:` + `go run` print).
- After: `reopenSucceededReducerWorkQuery` and
  `replaySucceededReducerDomainQuery` compose to byte-identical
  statements (`cmp` clean against the base canonicals).
  `reopenReducerWorkQuery` differs by exactly two lines: the
  `work.`-qualified `WHEN` refs become bare column names in a
  single-table UPDATE with no subquery, so they resolve identically.
- Backend/version: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2), local
  Docker, full migration set (184/184) via `TestAdminHandler_ReopenLive`.
- Input shape: succeeded reducer rows reopened to pending through
  `POST /api/v0/admin/reopen` (bounded locked id list), then claimed
  through the production claim path.
- Row counts: the live run reopened 2 rows and claimed both, one at a
  time on their shared conflict key in deterministic id order; a
  repeated call with the same idempotency key reopened 0 rows (no-op).
- Why safe: the emitted statements are equivalent to the base ones, so
  the claim/lease/retry behavior cannot change; the new pin test fails
  hermetically if any assignment is dropped from the clause (seeded
  violation: removing `claim_until = NULL` fails, restoring passes).

## No-Observability-Change (#7731):

No metric, span, log, or status output is added, removed, or renamed.
The reopen path emits the same statements as before, and the live
reopen run completed with the existing telemetry untouched; there is
no new operator-facing signal because there is no behavior change to
observe.
