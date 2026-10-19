# #6738 bootstrap lock_timeout knob (`ESHU_SCHEMA_LOCK_TIMEOUT`)

## Change and boundary

Issue #6738 fix 4 asks for a configurable/longer `lock_timeout` with bounded
retry and backoff. The retry/backoff half already landed (#6956: advisory-lock
ownership wait, per-run retry budget, doubling backoff; #7004: concurrent-index
exemption). What was missing is the `lock_timeout` value itself: a hardcoded
5 s constant no operator could tune. This lane adds `ESHU_SCHEMA_LOCK_TIMEOUT`
(`BootstrapOptions.LockTimeout`), parsed with the same refuse-garbage/zero/
negative contract as the two sibling knobs, threaded through
`schemaBootstrapCoordination` into the tracked migrator's per-statement
bounds. Unset keeps the exact previous behavior (5 s). Both bootstrap
binaries (`eshu-bootstrap-data-plane`, `bootstrap-index`) read it via the
shared `BootstrapOptionsFromEnv` hop with no wiring change. Shipped migration
bytes are untouched (the #7002 immutability manifest forbids editing them);
`002`'s bare `SET STATISTICS` stays as-shipped and the ledger skip covers its
re-run contention. `/readyz` never gated on the bootstrap Job (verified in
tree), so fix 5 needs no change.

## Performance Evidence

No-Regression Evidence: the default path is byte-identical -- an unset knob
resolves to the same 5 s constant through `withDefaults`, proven by
`TestSchemaBootstrapCoordinationLockTimeoutDefaults` and the end-to-end
`TestApplyBootstrapWithOptionsThreadsLockTimeout` fake-executor propagation
test. Live proof on a disposable PostgreSQL 18-alpine (container
`pg-6738-lock-knob`, DSN `postgresql://eshu:change-me@localhost:15991/eshu`,
throwaway in-package test run 2026-10-19, deleted before commit): the full
218-file tree applied with defaults in ~12.2 s and recorded 218 receipts; the
immediate rerun reported `applied=0 skipped=218` in ~42 ms. Under a held
`ACCESS EXCLUSIVE` lock on a proof table, a synthetic tracked migration with
an explicit 300 ms knob failed as `SQLSTATE 55P03` after 3.9 s and 3 attempts
against a 4 s retry budget (per-attempt waits of ~301 ms in the `lock_wait`
logs, so the knob demonstrably reached `set_config`); after the blocker
released, the same migration applied under the default knob in 33 ms and
recorded its receipt with the matching checksum. Longer timeouts queue for a
lock gap instead of failing fast; every failed attempt still consumes the
shared retry budget, so no new unbounded wait is introduced and the Job
deadline fit test (`ownershipWait + lockRetryBudget + work floor <=
activeDeadlineSeconds`) still passes unchanged. These are single local
samples on an empty disposable database, not throughput estimates.

Focused commands (lane cache/tmp under `/home/ubuntu/.cache/go-6738` because
shared `/tmp` was quota-exhausted by other lanes; their artifacts untouched):

```bash
cd go && go test ./internal/storage/postgres/ -run 'TestBootstrapOptionsFromEnv|TestSchemaBootstrapCoordination|TestApplyBootstrapWithOptionsThreadsLockTimeout' -count=1
cd go && go test ./internal/envregistry/ ./cmd/bootstrap-data-plane/ ./cmd/bootstrap-index/ -count=1
```

`scripts/test-verify-performance-evidence.sh` exits 0 (all self-tests pass)
and `scripts/verify-performance-evidence.sh` exits 0, reporting "no hot
Cypher/concurrency/runtime files changed" -- the knob addition trips no
gate pattern, and this note is the durable record regardless.

## Observability Evidence

No-Observability-Change: no new metric, span, log field, route, worker, or
queue is added. The knob's effect is visible through the existing migration
logs: each failed attempt logs
`bootstrap.postgres.migration.lock_wait` (with `attempt`, `backoff_ms`,
`waited_ms`, `budget_left_ms`, and the 55P03 error) and the eventual success
logs `bootstrap.postgres.migration.lock_recovered` with the attempt count --
both demonstrated in the live proof above. A misconfigured value fails closed
at startup with the variable named (same refusal contract as the sibling
knobs), before any statement runs.
