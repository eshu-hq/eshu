# Ops-QA readiness during reducer drain

Root-Cause Evidence: During the #6738 scratch-reset rollout on 2026-09-17,
five collectors, the webhook listener, and the workflow coordinator stayed
Running but 0/1 Ready. Direct `/healthz` returned 200 in under 1 ms; direct
`/readyz` returned 503 after the three-second application deadline while
`ReadStatusSnapshot` was listing stage counts or domain backlogs. An
`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` of the exact stage-count SQL was
canceled by a three-second `statement_timeout`. `pg_stat_activity` showed 15
concurrent active `active_fact_work_items` reads without lock waits, and the
Postgres pod was at its two-core CPU limit. The ops-qa Kubernetes readiness
probe allowed one second. Full snapshot aggregation on every service's
`/readyz` request therefore competed with reducer drain for Postgres CPU.

The status store now checks the latest compiled Postgres migration receipt by
path and checksum, and validates core status columns without scanning data rows.
The ordered migration runner writes each receipt only after the file succeeds;
the latest receipt is absent on a partial bootstrap. This keeps Postgres
connection and schema failures visible through `/readyz` without computing
queue, domain, or fact aggregates.
The API and MCP Postgres and graph probes remain unchanged. `/admin/status`
and `/metrics` still load full operator truth, including the projection backlog;
`/readyz` does not claim that the corpus is fully indexed.

No-Regression Evidence: On the same live ops-qa Postgres during reducer load,
the pre-review zero-row query planned in 1.669 ms, executed in 0.058 ms, and
scanned no table rows. Three direct client samples took 98.410, 99.136, and
201.727 ms. The first three-query version passed five pooled-connection live
checks in 595-699 ms, leaving little margin under the one-second Kubernetes
timeout; the single query removed two network round trips. The pre-review
checker passed five pooled-connection live checks in 297-395 ms. After the
receipt check was added, the final Go checker passed its read-only ops-qa live
test in 299.917 ms on a warmed connection under a one-second context. These
are query and checker measurements, not deployed service latency. A regression
test made the old `/readyz` path return 503 when full aggregation timed out; it
passes with the new schema checker and asserts zero full snapshot calls.
Focused tests cover missing schema, partial migration, missing receipt result,
canceled context, empty tables, wrapper forwarding, and fail-closed
construction without a checker.

Review follow-up: the initial zero-row query would have reported ready if
bootstrap stopped before migration 096, despite later queue and status paths
needing its `provenance_edge_identity_upgrade_required` column. The regression
was RED before the fix: a missing latest receipt and a missing receipt result
both returned success. Both now fail closed. A post-edit read-only
`EXPLAIN (ANALYZE, BUFFERS)` of the revised query on loaded ops-qa Postgres
used one primary-key index search on `eshu_schema_migrations`; the schema-check
branch scanned no rows (planning 0.247 ms, execution 0.058 ms). The exact
current receipt returned true and a wrong checksum returned false. A
deliberately misspelled status column failed at SQL analysis even though that
branch has `LIMIT 0`.

Observability Evidence: `/readyz` reports bounded schema failures under the
`status_schema` cause. The independent `postgres` and `graph` causes remain.
`/admin/status` and `/metrics` continue exposing backlog and health evidence.
During rollout, inspect PostgreSQL CPU and active status reads as well as pod
readiness: metrics scrapes still request the full report and can contribute
residual CPU load.
