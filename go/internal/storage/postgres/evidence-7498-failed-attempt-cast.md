# Evidence: failed-attempt upsert casts (#7498)

`upsertLocalIdentityFailedAttemptQuery` was rejected by Postgres at prepare
time (SQLSTATE 42804): `CASE WHEN 1 >= $2 THEN $3 ELSE NULL END` resolves `$3`
to text before the `locked_until` target column is considered. The fix casts
`$2::int` and `$3::timestamptz` in both the `INSERT ... VALUES` and the
`ON CONFLICT` branches. Query semantics and plan shape are unchanged: the
casts only fix parse-time parameter type resolution.

No-Regression Evidence: issue #7498 live proof on 2026-10-04 against
PostgreSQL 18.6 (local Compose project `eshu-7498-localid`, isolated schema
via `openIsolatedBootstrapSchema` applying all 178 bootstrap migrations).
Baseline: every failed-attempt record errored at prepare time with
`column "locked_until" is of type timestamp with time zone but expression is
of type text (SQLSTATE 42804)` — zero rows written, so no successful baseline
latency exists to regress. After the fix, `PREPARE` succeeds and the live
test `TestRecordFailedLocalIdentityAttemptLiveCountsAndLocks` passes: 5
sequential single-row PK upserts (input shape: one `user_id` PK row, attempts
1s apart) record terminal row counts `failed_attempts=5` with `locked_until`
set and equal to the 5th call's returned `LockedUntil`; per-call statuses are
invalid x4 then locked on the 5th, matching the DB ground truth. The statement
remains a single-row primary-key point-write with identical predicates and no
index DDL, so no plan or throughput change is expected; no benchmark delta is
claimed because the pre-fix path never succeeded.

No-Observability-Change: the error path is unchanged
(`record local identity failed attempt: %w` in
`identity_local_helpers.go`); the fix adds no spans, metrics, or logs. The
failure previously surfaced as a hard error, so successful attempts now emit
only the pre-existing success-path signals.
