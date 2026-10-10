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
and `failure_details` for every active work item. On the QA environment it processed about
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

Read-only QA proof on 2026-09-28 used `~/eshu-latency-kit/pgro.sh`, which
forces `default_transaction_read_only=on`. API and MCP served the same
`sha-83cbf62` image. The summary SQL source at that image matches the current
branch source before this edit. A temporary local package test emitted the
edited Go query; `cmp` confirmed it was byte-for-byte the measured candidate.
No QA DDL, `ANALYZE`, index, deployment, or application mutation was
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

## Follow-up status-only scan and terminal text (#7009, 2026-10-02)

The later `sha-4274e83` QA image still served a repository ingester-status
request in 1.453011 seconds. Its server trace spent 1.349317 seconds in the
status snapshot. The five-minute `active_work_summary` read histogram averaged
1.318449 seconds on that API pod. The histogram is aggregated across requests;
it does not attribute that exact trace to the summary statement.

### Performance Evidence:

The assembled deployed summary SQL, run in a read-only repeatable-read snapshot,
processed about 204,000 work rows and used roughly 42,200 temporary blocks
read and 10,550 written. An isolated projection-width shim reduced the measured
CTE storage from 84 MiB on disk to 18 MiB in memory, with zero temporary
blocks. The combined status-only scan fence and terminal text projection
returned the same nine ordered section rows as the original SQL in one shared
snapshot. One loaded paired run measured 1,508.638 ms for the original and
797.488 ms for the combined shim. These statement timings are a theory check,
not quiet-host endpoint p95 or a 100-user capacity result.

This follow-up applies that exact combined shape only to the status summary.
Its `OFFSET 0` work-input boundary enables the measured hash scan while the
shared generation predicate remains identical for other observers. Six more
text columns become NULL on succeeded and superseded rows: work, scope, and generation IDs;
domain; conflict domain and key. Stage, status, timestamps, provenance flag,
failure-row identity and details, stale-generation fencing, section ordering, and limits stay
as before. No queue write, lock, lease, schema, or index changes.

A disposable PostgreSQL 18.3 fixture first failed the production-CTE regression:
two succeeded rows retained consumer-only text. After the edit, the regression
and the existing summary-versus-standalone differential passed, including stale
and foreign generation pointers, blockages, failure ties, and section limits.
A temporary same-package accessor compiled the edited summary SQL from base
`b30e97189`; its SHA-256 and the measured combined shim's SHA-256 were both
`bef1078e2f5b971f6f35f576403b5c7197b59b6e37ccf13974d3240e4d432e2e`
(byte-for-byte equal). The shared observer FROM/WHERE SQL remained byte-for-byte
equal to base, SHA-256
`b48ac934a750e8946c708d3aa47bb47655eb1088d03122d50db8b805ff150791`.
The temporary accessor was removed. The fixture now includes one superseded
work row: stage counts retain it, queue total rises by one, and the summary
materializes neither of its consumer-only IDs or conflict strings. A seeded
unconditional ID projection produces three wide completed rows on that same
fixture. The blocking PostgreSQL CI job selects this regression explicitly and
sets `ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF=1`, making an unset DSN fail.
Reader-plan replay, API/MCP p95, and the full #7009 endpoint sweep remain
publication and deployment evidence for the coordinator to record.

No-Observability-Change: the existing
`eshu_dp_status_snapshot_read_duration_seconds{read="active_work_summary",outcome}`
continues to time this status read. The currently deployed API path passes a raw
read transaction into the store, so it has no per-query `postgres.query` child
span; use the labeled read metric and status snapshot span for this path.
