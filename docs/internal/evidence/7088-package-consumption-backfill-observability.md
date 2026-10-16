# Package-consumption sidecar repair observability (#7088)

Observability Evidence: The reducer's elected backfill pass emits a closed
`outcome` counter and duration for `contended`, `failed`, `incomplete`, and
`ready`. Successful elected passes update the last-success Unix time and readiness.
Capped dirty-scope count and initial-walk cursor update time refresh when
progress sampling succeeds.
Structured reducer pass logs carry the same bounded state. Storage's per-scope
key=value process logs retain scope detail without metric labels. A failed or contended
pass leaves the last known state gauges unchanged. A progress-sample error
warns without changing repair outcome or poll cadence.

Performance Evidence: The dirty backlog sample runs once after a successful
elected pass, never during Prometheus collection. Its SQL counts only the first
26 dirty rows; 26 indicates at least two 25-scope repair passes remain. On a
disposable PostgreSQL 18 session with 80,000 temporary dirty-scope rows
(primary-key `scope_id`), `EXPLAIN (ANALYZE, BUFFERS)` for the exact bounded
count showed a `Limit` returning 26 rows, 2 local buffer hits, and 0.024 ms
execution. The cursor sample reads one marker-keyed row. This is a bounded
probe, not a wall-time comparison with the prior reducer pass; no throughput
speedup is claimed.

Verification: `go test ./cmd/reducer ./internal/telemetry -run
'TestPackageManifestBackfillPassTelemetry|TestNewInstrumentsNoError|TestSecondsHistogramsHaveExplicitBuckets'
-count=1` exercises the four emitted pass outcomes and instrument registration.

Update (#7663): a pass no longer rebuilds a scope in one transaction. Each
scope gets a start transaction plus one transaction per 500-fact page. The
storage per-scope log lines now carry `outcome`, `pages`, `facts`,
`transactions`, `max_transaction_ms`, `lock_wait_ms`, `max_lock_wait_ms`, and
`duration_ms`, and a failed scope logs
`package_manifest_consumption_keys.backfill.scope_failed`. Three metrics
cover rebuild transactions, scope-row lock wait, and per-scope outcomes. See
[7663-backfill-page-lock.md](7663-backfill-page-lock.md).
