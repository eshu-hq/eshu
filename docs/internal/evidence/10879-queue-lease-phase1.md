# #10879 phase-1 evidence: dual-write cutover

Refs #10879. Part of #10860. Phase 1 keeps the main columns authoritative;
every number below is measured, not projected. Raw logs:
`/tmp/10879-migproof/migproof-*.log` (migration proof),
`/tmp/10879-probe-full.log` (claim EXPLAIN + index use).

No-Regression Evidence: phase 1 changes no main-table write. Baseline: the
#10879 census on the #10862 replay (8,713 rows, pre-change tree) reports
175 statements, 175 heap updates, 0 HOT (claim 40/0, renew 80/0, finish
15/0, retry 15/0, dead-letter 10/0, reopen 15/0). After: the same census on
this tree reports identical 175/175/0 — the main UPDATEs are semantically
unchanged; the only added cost is one lease-table INSERT/UPSERT/DELETE per
claim/heartbeat/terminal op. Backend: local PostgreSQL 18.6, disposable
schemas. Single-claim EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) paired on the
same seed with the lease CTE stripped for the before shape: before
5.872 ms / 2,200 shared hits, after 5.949 ms / 2,195 shared hits (single
samples, within noise; the only plan delta is the mirror self-join). The
phase-2 gate still requires a paired multi-sample comparison with a <10%
regression bound, because only the read switch can change the hot path.

Observability Evidence: terminal ACK/fail/retry statements now report
through QueryContext instead of ExecContext, so they emit postgres.query
spans and read-labeled durations instead of postgres.exec/write (the ACK
telemetry pin was updated to the real path; exception events, error
status, durations, and error identity are unchanged). Batch claim already
recorded its write as a read, so this matches existing practice; operators
keep the same `eshu_dp_postgres_query_duration_seconds` histogram and
`postgres.query` spans for diagnosis, and phase 2 revisits the whole
surface with the read switch.

## Migration proof (representative copy, 264,567 rows)

`scripts/dev/10879-lease-migration-proof.sh` on a disposable local PG 18.6
database with the QA row count (88,189 live claimed of 264,567):

| Phase | Wall time |
| --- | --- |
| `CREATE TABLE` (+FK, fillfactor 70, migration 317) | 2.8 ms (catalog-only) |
| Backfill `INSERT ... SELECT` 88,189 live leases (migration 318, shipped file) | 796 ms |
| `CREATE UNIQUE INDEX` (conflict unique, migration 317) | 0.7 ms (empty table) |
| Idempotent backfill replay with 200 concurrent heartbeats | 148 ms, heartbeats unblocked |
| 10x scale (2.65M rows): load 59 s, backfill 881,890 rows 19.2 s | linear, no lock errors |
| Rollback `DROP TABLE` | 167 ms; main row count unchanged 2,646,670 |

Exactness: mirror rows = live main leases = 88,189; orphans 0; re-apply is
idempotent. Sizes: main 137 MB, lease table 28 MB.

## Before/after on the #10862 replay (8,713 rows)

- Main-table writes by cause, before (prior census): 175 statements, 175
  heap updates, 0 HOT (claim 40/0, renew 80/0, finish 15/0, retry 15/0,
  dead-letter 10/0, reopen 15/0).
- Main-table writes by cause, after (this tree,
  `TestReducerQueue10879UpdateCensusByCause` re-run): identical 175/175/0 —
  the main UPDATEs are semantically unchanged, so phase 1 adds no main-table
  write and removes none. The added cost is exactly one lease-table
  INSERT/UPSERT/DELETE per claim/heartbeat/terminal op.
- Single-claim `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)`, paired on the same
  seed (lease CTE stripped for the "before" shape): before 5.872 ms /
  2,200 shared hits; after 5.949 ms / 2,195 shared hits (single samples,
  within noise; the only plan delta is the mirror self-join). The phase-2
  gate still requires a paired multi-sample comparison with a <10%
  regression bound.
- HOT share on the lease shape is proven by the prior cheapest-shim proof
  (0% -> 100% HOT on renewal-shaped updates); the mirrored heartbeat in this
  tree writes the same shape (claim_until-only UPDATE on a fillfactor-70
  row with no secondary index on the written column).

## Index use on the replay workload (10 claims + 10 heartbeats, per-schema)

| Index | Scans | Verdict |
| --- | --- | --- |
| `fact_work_items_pkey` | 14 | keep (claim + mirror join) |
| `fact_work_items_reducer_live_lease_uniq` | 4 | keep (inflight fence; phase-2 drop candidate) |
| `fact_work_items_reducer_conflict_claim_idx` | 1 | **keep in this lane** (touched by the claim path); drop ships as its own shim with since-reset stats (decision 5) |
| `fact_work_items_scope_generation_idx` | 8 | keep (stale-gate probes) |
| `fact_work_items_claim_until_idx` | 0 | keep (heartbeats write it; expiry scans use it at other scales) |
| `fact_work_items_cross_scope_source_v2_idx` | absent from bench schema | **keep in this lane** (serves the completion-fanout consumer in code; 0 QA scans cannot distinguish dead from stats-reset) |
| `fact_work_item_leases_pkey` | 1 | keep (mirror join + upsert arbiter) |
| `fact_work_item_leases_live_lease_uniq` | 0 | keep (no races in this workload; enforcement proven by the lease-unique test) |

## Concurrency proofs (committed tests)

`reducer_queue_10879_lease_test.go`: backfill exactness + idempotent
re-apply; claim/heartbeat mirror parity (owner + claim_until equal);
ack/retry/dead-letter leave no orphan (plus a full-table orphan count);
stale heartbeat AND stale ack rejected after expiry + reclaim with the lease
row on the new owner; lease-table unique refuses a second live lease and the
mapper still defers it. Open: container-image epoch variants under the
mirror and a true concurrent-claim race against the lease unique (phase 2,
with the read switch).

## Kill-statement shape (learned during verification)

The terminal statements first shipped as `WITH done ... DELETE` outer
(ExecContext, RowsAffected = deleted lease rows). The ACK lock-order proof
caught the hole: hand-inserted live rows have no mirror row, so a matched
main update reported 0 and a good ACK was falsely rejected. All six kill
statements now end `SELECT (SELECT count(*) FROM done)` and report the
main-table match count through `queryTerminalMatchCount`: success depends
only on the fence, never on mirror presence, while the mirror delete stays
in-statement and atomic. Side effect: these statements flow through
QueryContext (operation `read`, `postgres.query` spans), so the ACK/fanout
telemetry pin was updated to the real adapter path — the error proof
(exception events, error status, durations, error identity) is unchanged.
Precedent: the batch claim already records its write as a read.

## Fixture rule (established pattern, applied)

Every test fixture that runs queue writes applies
`MigrationSQL("fact_work_item_leases")`: the bench schema, the
supersede-readiness schema (covers supersede/sweep/coalesce/fanout-debt),
the cross-scope proof schema (covers shared-edge/pre-activation), the
liveness proof schema, and the CICD-history schema. Fixtures that run the
post-ACK producer wake also need `reducer_waiting_items`; that gap
predates this lane (see below) and is left for its owner.

The retention row-count query gained the `fact_work_item_leases` arm
(same shape as the #8300 fingerprint arm): the coverage test enumerates
every cascade child from the catalog and fails on a missing arm, so the
new table had to be registered. Verified green.

## Not regressions (proven on the untouched lane base)

The full blast-radius run (371 passing, telemetry pins fixed after that
run and verified green in isolation) leaves 14 failures that fail
byte-identically on a clean `ef00153` worktree with none of this lane's
changes: 10 wait/reclaim claim-misses (cross-scope, pre-activation x2,
security-alert x2, service-catalog x3 incl. the 60s worker timeout,
shared-edge, readiness-wait) + 4 missing-`reducer_waiting_items`
(deferred backfill x2, 10862/10863 replay). The waiting-items family is the
known #11013 gap; the claim-miss family is a wait/settle behavior on this
base, untouched by the lease split (no reader changed).

Outside the blast radius: `webhook/actions` and 3 `vulnerability` tests
failed only under a keyword/value DSN (their harness appends
`?search_path=`, which mangles a keyword DSN into a bad socket path) and
pass with a URL DSN — environmental, no product surface involved. Two
retention fact-key tests fail identically on the untouched base: they pin a
3-table map while migration 315 (parallel #10875 lane, already in base)
adds a fourth fact-key table — not this lane.

## Deliberately not done here

Heartbeat narrowing, the claim/liveness/reproject/reopen/fanout read
switch, dropping the main-table lease columns or unique, cheaper-alternative
shims, and the QA-scale rerun — all phase-2 or follow-up lanes per the
decision record. Projector writers are unwired pending a projector
lease-write census (phase 1b).
