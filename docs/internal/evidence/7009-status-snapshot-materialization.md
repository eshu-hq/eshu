# Status snapshot materialization width (#7009)

## Scope and cause

The API and MCP status snapshot reads share `StatusStore.ReadStatusSnapshotFiltered`.
Its `active_work_summary` statement builds stage counts, domain backlog, queue
state, conflict blockage, and latest failure from one materialized active work
set. On the owner-deployed `sha-83cbf62` image, API metrics recorded 3,235
successful summary reads averaging 1,468.67 ms, with 2,095 over one second.
MCP recorded 3,223 averaging 1,639.79 ms, with 2,300 over one second. The
next largest read, `terraform_state`, averaged 389.72 ms on API and 439.34 ms
on MCP. These cumulative histograms identify the summary as the dominant
status read; they are not paired endpoint samples.

The deployed SQL materialized `payload`, `failure_class`, `failure_message`,
and `failure_details` for every active work item. On ops-qa it processed about
122,715 work items, wrote 10,826 temporary 8-KiB blocks, and reread 43,304.
Most rows are terminal history, so their wide detail is unused by the summary.
This is a broad materialization and repeated temp-read cost, not evidence of
queue lock wait.

## Change and semantics

The active set still materializes once and keeps the same joins, filters,
sections, ordering, and limits. It carries payload only for reducer rows in
`pending`, `retrying`, `claimed`, or `running` state, a superset of rows whose
payload can be read by the blockage section. It carries failure text only for
`retrying`, `failed`, or `dead_letter` rows, the only states considered by the
latest-failure section. Other rows project SQL NULL in those four fields.
The original base-table scan and absence of per-row detail lookups are
preserved, including when a busy queue makes most rows eligible. No lock,
lease, claim, write, index, or schema behavior changes.

The existing PostgreSQL fixture differential covers stale generations, empty
and foreign active pointers, readiness and lease blockages, duplicate conflict
keys, blockage limits, failure ordering, and output parity. The lease-plan
regression still rejects repeated scans with missing, current, and stale
statistics. A shape test checks the production SQL projection and rejects a
seeded `work.*` wildcard violation.

## Performance Evidence:

Read-only ops-qa proof on 2026-09-28 used `~/eshu-latency-kit/pgro.sh`, which
forces `default_transaction_read_only=on`. API and MCP served the same
`sha-83cbf62` image. The summary SQL source at that image matches the current
branch source before this edit. A temporary local package test emitted the
edited Go query; `cmp` confirmed it was byte-for-byte the measured candidate.
No ops-qa DDL, `ANALYZE`, index, deployment, or application mutation was
performed; the wrapper set read-only mode for its session.

One `REPEATABLE READ READ ONLY` transaction bound both prepared statements to
`CURRENT_TIMESTAMP` (2026-09-28 11:58:02 UTC). Baseline and candidate each
returned the same 11 ordered section rows; both hashes were
`d27f0f9872439bad3720d8f4570f68cce4c7ec172a633dd609b66305a002a965`.
Three interleaved `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON)` pairs
on that snapshot gave:

| SQL | Execution time (ms) | Median (ms) | Temp read/write blocks |
| --- | --- | ---: | ---: |
| Deployed baseline | 1096.248, 929.998, 853.720 | 929.998 | 43,304 / 10,826 each |
| Edited Go query | 704.458, 736.536, 836.551 | 736.536 | 0 / 0 each |

The median statement reduction was 193.462 ms (20.8%) on this snapshot.
The server-side plan time does not include API/MCP response work.

A disposable local PostgreSQL 18 fixture with 814 scopes, 17,094 generations,
and about 114,000 **pending reducer rows** tested the busy-queue edge. Three
interleaved pairs measured baseline 376.802/442.251/415.016 ms (median
415.016) and candidate 376.630/395.582/467.522 ms (median 395.582).
Both plans read/wrote 77,215/30,676 temp blocks: conditional projection
cannot narrow a set where nearly every row needs payload. This one fixture
showed no median regression; one candidate sample was 467.522 ms. It is not
a production throughput guarantee. A rejected late-primary-key-lookup variant
measured 1,432.475 versus 251.298 ms on a prior synthetic busy queue, so it
was not shipped.

## Verification and observability

- Regression first: `go test ./internal/storage/postgres -run '^TestActiveWorkSummaryProjectsWideFieldsOnlyForConsumers$' -count=1` failed on the original unconditional payload projection, then passed after the edit. Its seeded wildcard positive control also fails the projection guard.
- The local PostgreSQL fixture differential and lease-plan tests must pass on the final branch. Record their final commands and results in the PR.
- No-Observability-Change: existing `eshu_dp_status_snapshot_read_duration_seconds{read,outcome}` and `postgres.query` span `db.query.summary` continue to attribute this read. Operators can compare the same signal after owner deployment.

The statement result does not establish a below-one-second endpoint p95 or
resolve the other #7009 endpoint families. Owner deployment and a comparable
cold/warm sweep remain required before closing the issue.
