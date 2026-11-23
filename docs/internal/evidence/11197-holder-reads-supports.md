# #11197: supply-chain impact holder reads move to the supports table

Refs #11197, #10880 (PR-B2b). The reopen fan-out family (holder read, key-range
statements with the foreign-member token arm, mass probe), the repository holder
read, the scoped finding-package seed read, the out-of-stamp probe and the
go-modules fence read `reducer_supply_chain_impact_finding` facts. Once PR-C stops
writing those facts they must answer from `supply_chain_impact_finding_supports`
(migration 338). This note records the proofs behind the move: the plans, the WAL
cost of the four new indexes, the readiness rule and the equality tests.

## The five reads, by symbol

| Read | Statement | Arms and indexes on supports |
| --- | --- | --- |
| Fan-out holder read | `FindSupplyChainImpactHoldersQuery` | `cve_id`, `advisory_id` (new btrees 339, 340), group members (new GIN 341) |
| Key-range and open-range holder statements | `FindSupplyChainImpactHolderKeyRangeQuery`, `FindSupplyChainImpactHolderOpenKeyRangeQuery` | `cve_id`/`advisory_id` with a `fact_id` range (339, 340), foreign-member token arm (new GIN 342 on the frozen `eshu_sci_foreign_member_tokens_v4`) |
| Holder mass probe | `HolderMassProbeQuery` | the three arms of the holder read, per lookup id with `LIMIT` |
| Repository holders | `findRepositoryFindingHoldersQuery` | existing `supply_chain_impact_finding_supports_repository_idx` |
| Scoped seed | `listScopedSupplyChainImpactFindingPackagesQuery` | existing `(scope_id, generation_id)` index |
| Out-of-stamp probe | `hasOutOfStampActiveSupplyChainImpactRowsQuery` | existing `(scope_id, generation_id)` index |
| Go-modules fence | `fenceFindHoldersQuery` | existing repository index |

Equality details the tests pin: the supports `ecosystem` column is lowercased for
filtering, so the seed read takes `ecosystem`, `purl` and `package_name` from the
verbatim support payload; supports store `''` for a finding without a CVE,
advisory or repository id, so every holder read excludes the blank id (the fact
read's `->>` equality never matched a missing key) and the store drops blank
lookup ids before any statement.

## Theory proof: the plans (before any code)

Disposable `postgres:18`, bootstrap schema, 300,000 impact facts across 3,000
scopes (a hot CVE with 30,000 rows, five mid CVEs of about 600, a hot foreign group
member, 10% of rows in superseded generations), supports filled by the real
backfill statement: supports table 469 MB with 38 MB of B1 indexes; `fact_records`
469 MB with 456 MB of indexes. `EXPLAIN (ANALYZE, BUFFERS)`, third of three runs.

| Statement (200-id chunk unless named) | Facts: buffers, ms | Supports, B1 indexes only | Supports, with the four indexes |
| --- | --- | --- | --- |
| Holders | 1,932, 2.8 | 192,058, 590 (three seq scans) | 2,135, 4.3 after a GIN flush |
| Holders, hot singleton | 30,517, 37.7 | 192,058, 172 | 26,868, 35.2 |
| One key-range statement of 16 (with the token arm) | 1,952, 4.1 | 72,718, 3,306 | 3,335, 5.2 |
| Repository holders | 112, 0.64 | 118, 0.59 (existing index) | unchanged |
| Scoped seed | 126, 0.079 | 112, 0.043 (existing index) | unchanged |
| Out-of-stamp probe | 4, 0.014 | 8, 0.009 (existing index) | unchanged |

Without the four indexes every arm of the fan-out read seq-scans the table, so the
fan-out family cannot move without them; the four other reads need no schema
change. A first run read 33 ms for the members GIN probe because the 20,000 test
inserts had left an unflushed GIN pending list; after `gin_clean_pending_list` the
probe reads 0.2 ms. Scratch generator: not committed (arbiter-b2b).

## Writer WAL cost (W-A, the real writer)

Harness: the B1 write-cost harness shape on `PostgresSupplyChainImpactWriter` (3,000
findings: insert, replay under a new intent id, retract 1,500), a fresh isolated
database per (arm, rep), cold (`CHECKPOINT` before each pass) and warm (after a priming
insert, replay and retract) cells, `pg_stat_wal` deltas of the whole WAL bracketed with
`pg_stat_checkpointer` (a checkpoint inside a pass voids it; 0 voids in every run
below), arms rotated per rep, server `checkpoint_timeout` 30 min, `max_wal_size` 8 GB,
autovacuum off, volume after the I/O change. S1 has no group members; S2 gives every
finding one member shared by all 3,000 and one of 50 shared ids. Arms that keep a
default-fastupdate GIN flush it with `gin_clean_pending_list` inside each pass, and
every arm with the members GIN off asserts 0 pending pages. Raw: `wa-all.txt`,
`wa-gin.txt`, `wa-final.txt`, summaries by `wa_sum.py`.

**Shipped set** (339 and 340 plain btrees, 341 `fastupdate = off`, 342 default), built
from the unmodified migration files, 10 reps, spread 0.0% in every cell, points over the
same-session B1-shipped median (stops: +30 insert, +36 replay):

| Cell | Insert | Replay | Retract (no stop) |
| --- | --- | --- | --- |
| S1 cold | +17.3 | +5.3 | +2.9 |
| S1 warm | +5.2 | +6.0 | +4.4 |
| S2 cold | **+28.9** | +17.2 | +31.6 |
| S2 warm | +22.6 | **+31.9** | +28.3 |

The worst cells are 1.1 and 4.1 points inside the stops. This is **above the epic's
+25% impact-WAL rule**; the rule is not met or moved, and the QA window W1b decides
before B3. It reproduces the earlier five-rep run of the same set (`mo`) to 0.00 points
in every cell. Pass time against the default-fastupdate arm is 0.99 to 1.03 in every
cell (return line 1.15).

**Why `fastupdate` is off on one GIN only.** Ten-rep run of ten arms, no INCLUDE
(`nd` both GINs default; `no` both off; the legend of the first run named `co` the
covering set and built `d`, `e`, `g` on it, so only `fn` is on the final set):

| Cell | `nd` | `no` | `mo` members off only | `to` tokens off only |
| --- | --- | --- | --- | --- |
| S2 cold insert | +23.0 | +28.3 | +28.9 | +22.3 |
| S2 cold replay | +16.5 | +19.9 | +17.2 | +19.2 |
| S2 warm insert | +17.1 | **+39.0** | +22.6 | +33.5 |
| S2 warm replay | +22.5 | **+54.2** | +31.9 | +44.9 |

With both GINs off the S2 warm cells breach both stops. The excess is additive per
index: S2 warm insert members 2,065,306 B (688 B per finding) and tokens 6,186,833 B
(2,062 B), replay 2,477,852 B and 5,896,148 B, so the tokens GIN carries 75% of the
insert gap and 70% of the replay gap. The members GIN is the one whose pending list
flipped the mass probe, so it is the one that ships off. Shape split (S2 warm, `no`
minus `nd`, insert / replay, points; the split run puts S2 first in its session, so its
S2 baseline differs from the W-A run's and only the differences inside one row compare):
S2 +27.0 / +29.4; one of 50 shared ids and no hot member (S2y) +41.2 / +0.8; own members
only (S2x) -6.0 / -6.1; six unique members (S2z) -4.9 / -5.3. The cost needs a member shared by many findings; the one-of-50 ids
(60 rows per id per pass) carry the insert excess without the member on every row. QA's
largest alias is on 0.6% of rows.

**The B2c and B3 bars.** B2c reads the final set against `fn` minus `no` (measured on the
both-off base, with the btree deltas assumed independent of the GIN setting: not measured;
B2c's own PR states its miss): S1 cold +4.7
insert / +5.2 replay, S1 warm +5.0 / +4.7, S2 warm +4.0 / +3.9 (met), S2 cold +10.2 / +7.8
(missed; 398 full-page images, 3.26 MB of the 4.21 MB insert difference, and 551 on replay). B3 (arm `f`, the shipped set plus 345, 343, 344 and the repository swap, below
twice the writer without supports): measured `f` on the shipped set reads 1.46 to 2.06
on medians, with S2 warm replay at 1.94. Three S2 cold cells have no stable `f` value
(bimodal, below), and the pre-declared rule judges them on the higher value: insert
57,428,212 B on 8 of 10 reps (f/a 1.90, 1 more at 57,428,266) and 68,508,532 B on 1 (2.27);
replay 66,454,472 B on 9 reps (1.86) and 77,533,752 B on 1 (2.17); retract 78.91 MB on 8
reps (2.06) and 67,831,219 B on 2 (1.77). So the B3 miss is three cells (2.27, 2.17, 2.06),
not one. It returns to the arbiter before B3; PR-C's derived S2 warm replay is +31.9 + 3.9 = +35.8 against a break-even of
about +28.

**Bimodal cells.** Eleven cells (S2 cold replay and retract) did not reach 5% spread in
ten reps: one fixed block of 3,027 full-page images (11.08 MB) lands in the replay
window, the retract window or neither, so the three-pass sum takes two values 11.08 MB
apart (`fn` 193.96 or 205.05 MB; `nd` S2 cold replay 9 reps at 61,958,855 B and 1 at
73,042,399 B; `no` 10 of 10 at 63,772 kB). The medians land on the likelier value and
`b1` and `no` are resolved in all 12 cells.

HOT updates: the replay pass wrote `n_tup_upd` 9,000 with `n_tup_hot_upd` 0 in every
variant, B1-shipped included. The only stop for the supports writes is an image
rollback; there is no runtime switch for the writes. The pre-declared flip for the GINs
is `ALTER INDEX ... RESET (fastupdate)` on 341, or `SET (fastupdate = off)` on 342, each
returning with both W1b windows and writer transaction p95 and max.

## Readiness

Every read answers only from a ready supports table: the backfill marker for the
binary's contract version complete, no marker for a newer version, and an empty
old-writer fence (`supplyChainImpactSupportsReadySQL`, contract-pinned). The
check is the shared B2c commit's `RequireSupplyChainImpactSupportsReady`; it runs
as the first statement of the transaction of each repeatable-read read (the key-range
read, the mass probe, the holder statement and the repository holder read), in the
snapshot of the read, so a table that goes not-ready cannot be read half-way. The
go-modules fence is the exception: it runs at READ COMMITTED, so its check and its
holder read are not one snapshot, and an old-writer image marking a scope dirty between
them can let a fence retract with a holder missed. The race needs a mixed-version deploy
during the backfill, and the fence's own re-run and the backfill's dirty-scope drain
bound it; it is stated here and in the PR body, not closed. A table that is
not ready refuses the read with `SupplyChainImpactSupportsNotReadyError`; there is
no fall back to the finding facts.

- The fan-out holder, key-range and mass reads and the repository holder read
  return the typed error unwrapped past `fanOutError` (`fanOutReadFailure`), so
  the queue defers the item without counting an attempt (class
  `supply_chain_impact_supports_not_ready`, expected wait). Every other read
  failure keeps `supply_chain_impact_fanout_failed` and counts.
- The scoped seed and the out-of-stamp probe keep the existing rule: a failed
  read runs the pass full (the seed-failure warning carries `supports_not_ready`).
- The go-modules fence takes the check right after its timeouts and before the
  pointer detach (`Store.WithSupportsReadiness`, required by
  `FenceStaleGoEvidence`), so a table that is not ready rolls the fence back with
  nothing retracted, and the coordinator retries the page.
- The supply-chain impact handler asks its fan-out port for the same check
  before the manifest marker, any evidence load, the fencing token, the writer or
  any holder read (`SupplyChainImpactSupportsGate`, `beforeEvidenceLoad`), so a
  table that is not ready defers the whole pass instead of failing it after the
  writer committed. `TestHandlerDefersBeforeAnyWorkWhileSupportsAreNotReady` pins
  zero writer calls, zero tokens and zero loads on the refusal. The per-read
  store check stays as the backstop that reaches the queue unwrapped.

Operators watch `eshu_dp_supply_chain_impact_supports_reads_total` (`source` label:
`fanout_holders`, `fanout_holders_key_range`, `fanout_holder_mass`,
`repository_holders`, `scoped_packages`, `out_of_stamp`, `go_modules_fence`;
outcome `served`, `not_ready` or, for a failed readiness probe only, `failed`). A sustained `not_ready` rate means the backfill
is unfinished or an old-writer image is still running.

## Plans after the move (final indexes, VACUUM ANALYZE)

Disposable `postgres:18`, bootstrap schema with migrations 339 to 342, 100,000
impact facts across 2,000 scopes (hot CVE at 10% of rows, a hot foreign group member,
superseded generations), supports filled by the real backfill, then
`VACUUM (ANALYZE)` on both tables. The vacuum flushes the GIN pending list, which
this table of plans needs only because it was measured before `fastupdate = off`
shipped; see "The pending list" below for what an unflushed list does. `fact_records` 156 MB with 218 MB of indexes;
supports 156 MB with 106 MB of indexes. `EXPLAIN (ANALYZE, BUFFERS)`, third of three
runs, the retired fact statements against the shipped supports statements.
Host load average 20 to 40 during the run, so the times are indicative; buffers are
not load dependent.

| Statement | Facts buffers, ms | Supports buffers, ms |
| --- | --- | --- |
| Holders, 200-id chunk | 1,553, 3.14 | 1,535, 2.98 |
| Holders, 5-id production statement | 81, 0.51 | 80, 0.35 |
| Holders, hot singleton | 10,266, 8.88 | 2,210, 4.95 |
| Key-range, hot range | 695, 1.46 | 384, 1.26 |
| Open key-range, last range | 664, 1.27 | 326, 1.22 |
| Mass probe, 5 ids | 366, 1.81 | 79, 0.67 |
| Repository holders | 63, 0.41 | 61, 0.37 |
| Scoped seed | 53, 0.052 | 12, 0.028 |
| Out-of-stamp probe | 4, 0.014 | 3, 0.009 |

No supports statement reads more buffers than its fact statement (the arbiter bar was
within 10%). The hot singleton and the mass probe read fewer: the supports rows are
narrower and the hot id's rows sit in one btree range. The scratch generator and
harness are not committed (arbiter-b2b).

## Index build cost

`CREATE INDEX CONCURRENTLY` on a filled supports table, one run per scale, host load
average 25 to 30 (single runs; the times carry that noise):

| Supports rows (table) | cve | advisory | members GIN | tokens GIN |
| --- | --- | --- | --- | --- |
| 25,000 (39 MB) | 130 ms, 2.2 MB | 118 ms, 1.6 MB | 96 ms, 2.0 MB | 476 ms, 2.6 MB |
| 100,000 (156 MB) | 551 ms, 8.6 MB | 231 ms, 6.5 MB | 227 ms, 8.6 MB | 1.61 s, 10.1 MB |

Four times the rows costs about four times the size and 2.0 to 4.2 times the time. A
concurrent build takes `SHARE UPDATE EXCLUSIVE`, which does not block the writer.

Backfill rate with the four indexes present: 1,040 rows/s at 100,000 rows with host
load 25 to 35. Earlier paired runs under the same load swung between 502 and 3,227
rows/s with and without the indexes, so this host cannot resolve the index cost on
the backfill. The quiet re-run against 2,394 rows/s is NOT_CHECKED. The writer-side
index cost is measured by the WAL table above.

## Proofs in the tree

| Claim | Test |
| --- | --- |
| Supports statements equal the retired fact statements on one corpus, and keep answering after every finding fact is deleted | `TestSupplyChainImpactSupportsHolderFamilyEqualsTheFactOracleLive` |
| Repository holders, scoped seed and out-of-stamp equal their fact reads and refuse a table that is not ready | `TestSupplyChainImpactSupportsHolderReadsEqualFactReadsLive`, `TestSupplyChainImpactSupportsHolderReadsRefuseNotReadyLive` |
| Every holder statement reads its supports indexes by name with no sequential or primary-key scan; four seeded index drops each fail the check; the generic plan fails it | `TestSupplyChainImpactSupportsHolderPlanShapeLive` |
| Holder reads, the real writer and a cascade-deleting scope run together with no read error, no deadlock, no torn replacement | `TestSupplyChainImpactSupportsHolderReadsUnderWriterContentionLive` |
| The fence checks readiness before it detaches, and keeps the #11147 reopen order | `TestFenceStopsBeforeDetachingWhenSupportsAreNotReady`, `TestFenceReopensHoldersInWorkItemLockOrder`, `TestFenceReopensHoldersInDeadLetterLockOrderLive` |
| The typed refusal reaches the queue unwrapped | `core/fanout_supports_not_ready_test.go` |

Performance Evidence: the plans table above (supports buffers at or below the fact
statements on every moved read, final indexes, `VACUUM (ANALYZE)`), the W-A table
(shipped set: worst cells S2 cold insert +28.9 and S2 warm replay +31.9 over
B1-shipped, inside the +30 and +36 stop lines; above the epic's +25% impact-WAL rule,
which this note names), and the 8M plan and flush proofs below.

No-Regression Evidence: the holder statements keep their shape and their chunking,
the fence keeps the #11147 reopen order, no lease or lock path changes, and the
readiness check is one read of the marker and dirty tables per transaction. The queue
path does change, in the shared B2c commit this PR carries: the not-ready failure class
`supply_chain_impact_supports_not_ready` is enrolled in `nonCountingReducerRetryFailureClasses`
(`reducer_queue_readiness_sql.go`), which changes the claim SQL bytes and re-pins the two
claim-gate digests (`reducer_claim_stale_gate_11234_guard_test.go`); a
`WaitingDeferralMap` row names the class; and the handler-entry gate defers every impact
pass, not only the holder reads, until the supports table is ready, which pauses impact
passes during the backfill (the telemetry doc states it). The regression is
`TestReducerQueueFailDefersSupplyChainImpactSupportsNotReadyPastAttemptBudget`: the class
defers past the attempt budget without a dead letter.

Observability Evidence: `eshu_dp_supply_chain_impact_supports_reads_total` (`source`
carries the seven new readers; `outcome` `served`, `not_ready` or `failed`, where `failed` is a readiness-probe failure only; statement timeouts show on the fan-out read histogram and the dead-letter class), the existing
`eshu_dp_supply_chain_impact_supports_backfill_ready` gauge and the shared
not-ready failure class `supply_chain_impact_supports_not_ready`; the fence emits the
same counter with `source=go_modules_fence`.

## 8,000,000 supports: scale, the pending list, hot ids

Disposable `postgres:18.6` in a 6 GB container, `shared_buffers` 2 GB, `work_mem`
64 MB, autovacuum off; 8,000,000 supports (8.9 GB table, 3.5 GB of indexes: cve
856 MB, advisory 687 MB, members 207 MB, tokens 381 MB), a hot CVE on 20% of rows
(1.6M rows) and a hot foreign group member on 10%. Raw logs: `scale3.txt` (states),
`r1-cve-cold.txt`, `r1-cve-warm.txt`, `r1-adv.txt` (16-range reads). Plain builds of
the four indexes under load took 387, 201, 176 and 201 s as `CREATE INDEX
CONCURRENTLY`; the writer is not blocked.

### The pending list

A GIN with `fastupdate` on batches inserts in a pending list that the planner prices
into every scan. The mass probe asks each id for at most 101 rows through a per-id
`LIMIT`; for an unknown parameter the planner assumes 1% of the table matches and
prices a sequential scan under `LIMIT` at about 10,000 heap rows, a constant (1,594
at 8M). The bitmap side grows with the table and with every pending page (about 4.25
cost units each). Plan-only measurements of the shipped probe as the members pending
list grows: 8, 34, 65 and 129 pages stay on the members index; 257 and 400 pages
flip to a Seq Scan. Probe of five ids, hot id included:

| State | Probe | 5-id holders (no hot id) |
| --- | --- | --- |
| Flushed (VACUUM) | 291 ms | 0.84 ms |
| Default list, 480 of 512 pages | 254,293 ms, members arm Seq Scan 50.9 s per id | 5.3 ms |
| Same, no hot id | 355,699 ms | |
| `fastupdate = off` | 252 ms | 0.78 ms |
| `gin_pending_list_limit = 64 kB` | 245 ms | 0.90 ms |

These rows were measured with `fastupdate` on, the default, and describe what the
option removes; the flip between 129 and 257 pages is a derivation from the planner's
costs, not a property of the shipped configuration. The members GIN (341) ships with
`fastupdate = off`, so there is no list for the planner to price. A fence around the
member arm (`OFFSET 0`) does not change the plan (Seq Scan, 686 s); a `MATERIALIZED`
CTE does (470 ms) but reads every match of a hot member, the read the `LIMIT` exists
to prevent, so it is not shipped. The mass-probe transaction sets
`enable_seqscan = off`, transaction-local, for the cost race that survives the list:
the sequential side is constant (about 10,000 heap rows under `LIMIT`) and the GIN side
grows with the table, a 1.71x margin at 8M, so a table a few times larger flips the
probe with an empty list. The holder and key-range statements keep the planner's own
choice. The tokens GIN (342) keeps the default list; see the W-A section for why.
`TestSupplyChainImpactSupportsMassProbeSurvivesPendingListLive` pins this: RED-1 (the
members option reset, both lists at 411 and 407 pages, no setting) must be a Seq Scan;
under the shipped options the members list stays empty and the member arm is on its
index; under the helper's settings in the RED state the arm is on the index and the
probe's masses equal the flushed run's; with both lists full the 5-id statement and both
key-range statements keep every arm on its named index.

**Proofs at 8,600,000 supports for the shipped set** (tokens list filled to 500 of 512
pages, members list 0; both plan modes; each statement run twice, warm = second, then
again after `gin_clean_pending_list` on the tokens list; raw `p1-full.txt`,
`p1-flushed.txt`, `p1_sum.py`). P1, plans: bounded range 8 and open range 16 for the hot
member and for a 400-row member, the 5-id holder statement and the mass probe under the
helper's setting: every arm on its named index, no Seq Scan on supports, no planner
setting on the key-range and holder statements; warm full-list time minus flushed time
at most +8.0 ms (50 ms line). P2, flush: `gin_clean_pending_list` on the limit-full
tokens list three times, refilled between runs: 0.487 s / 49.3 MB of WAL, 0.845 s /
182.8 MB, 0.502 s / 49.2 MB, on a quiet host with a warm cache. Cold: the list filled to 500
pages, a checkpoint, a container restart (cold `shared_buffers`), one flush: 2.545 s / 183.0 MB
of WAL (399 MB read), then 0.755 s and 0.729 s on two repeats. The line is a quarter of the smaller of
the work-item lease (`reducerClaimLeaseDuration` = 1 min, `cmd/reducer/main_helpers.go:49`)
and the handler timeout (`ESHU_REDUCER_HANDLER_TIMEOUT` unset is unbounded,
the `ESHU_REDUCER_HANDLER_TIMEOUT` entry in `go/internal/envregistry/entries.go`; `10643-holder-read-bound.md:96` records a `supply_chain_impact=30m` override read on the QA
deployment, which this PR does not re-check; the line holds on the 1 min lease alone): 15 s; the warm maximum is 0.845 s and the cold maximum 2.545 s. An earlier flush
figure of 12 to 15 s was taken with `fastupdate` on both GINs and no raw output was
kept; it is not a measurement of this configuration.

### Hot ids: the key-range read is bound by heap pages

The 16-range read with the hot CVE (1.6M rows) on the shipped indexes, plan checked
per statement (each arm on its index with both `fact_id` bounds in the Index Cond,
lower only on the open range; no Seq Scan; zero temp written: all 16 pass):

| Run | Range 1 | Ranges 2 to 5 | Ranges 6 to 16 | Sum |
| --- | --- | --- | --- | --- |
| Cold (container restart) | 97.7 s | 65.6, 39.5, 28.1, 15.2 s | 7.3 to 13.7 s | 351 s |
| Second pass | | | 7.3 to 22.5 s | 153.7 s |
| Hot foreign member (token GIN arm), cold | | | 21.8 to 84 s per range | 710 s |

The earlier "117 ms" for a hot-id range was one range repeated three times, not the
read: a range's 100,490 hot rows sit on about 87,000 distinct heap pages, so the 16
ranges together touch about the whole 9 GB table, more than the 6 GB the container
caches, and every pass reads 15,000 to 20,000 pages per statement from disk. The
store's statement timeout is 10 s; a timeout is `ErrFanOutReadTimeout`, a retryable,
counting failure with a budget of 3 attempts, and each attempt restarts at range 1, so a
cold id above capacity dead-letters, as the fact read does on QA today.

### Capacity limit

The cve and advisory arms fetch about 0.75 heap pages per matching row. Lab at 8M, about
100,000 rows per statement: 44k-75k pages and 7.7-11.7 s cold on a quiet host, 97.7 s on
a loaded one; 127-195 ms warm. An id above capacity times out at 10 s, retries three times
and dead-letters under `supply_chain_impact_fanout_failed` with the owed marker kept.
QA's hottest id is 83,642 rows, about 5,200 per statement: 0.6 s per statement at the
quiet lab rate, 4.9 s at the loaded rate (derived, not measured on QA). The lab hot ids
are synthetic at 19 and 16 times QA.

The token and members arms fetch about one heap page per matching row. Lab at 8M:
42k-52k pages and 22-84 s per statement for a member in 800,000 rows. A member above
capacity times out, retries three times and dead-letters, as the fact read does today.
QA's largest measured alias is 49,133 rows, about 3,100 rows per statement.

### Covering `INCLUDE (scope_id, generation_id)`: measured, not adopted

The cve and advisory arms select only `scope_id` and `generation_id`
(`supply_chain_impact_fanout_key_range.go:31,48`), so a covering btree can serve them
index-only. Measured on the 8M table against the plain indexes (raw `c1-*`, `c2-*`,
`c3-*`, `c4-*`):

| Check | Result |
| --- | --- |
| Plan, 16 hot-CVE statements, after VACUUM | Index Only Scan on both covering indexes, id and both bounds in the Index Cond (lower only on the open range), no Seq Scan, temp 0 |
| Footprint after VACUUM | Heap Fetches 0; 1,918 to 2,005 `shared read` per statement; through the store 1.756 s with a 195 ms worst statement, 0 timeouts (pass 2: 0.684 s) |
| Natural state, a vacuum several hundred thousand inserts old | Heap Fetches 3,329 to 3,500 per statement |
| 5% of rows rewritten, no VACUUM (Heap Fetches 30,088 to 30,284 per range, 30% of rows) | cold execution covering 15.3 / 12.8 / 10.9 s against plain 11.6 / 11.7 / 7.7 s in ranges 1 / 8 / 16: slower in all three, over the 10 s guard in all three; 28,814 pages at 0.52 ms per page against 74,436 pages at 0.14 ms per page (range 1) |
| 5-id statement, mass probe, hot foreign member range | within 10% `shared read`, not slower warm |
| Size and build | +49.6% (cve) and +47.5% (advisory) index bytes; concurrent builds 85.9 s and 72.4 s against 57.9 s and 54.0 s on a quiet host |

Visibility is per page, so 5% churn leaves about 30% of rows on not-all-visible pages
(1 - 0.95^7 at about 7 rows per page); the cold crossover is about 22 to 28% of the id's
rows on such pages (three ranges, one session, this device). QA shows 7.9% dead tuples
during the backfill and a snapshot held for over an hour that keeps vacuum from setting
pages all-visible, so the benefit depends on a state nobody maintains, the cost (index
bytes, build time, an unmeasured share of the insert headroom) is paid on every write,
and no QA shape needs it today. The time is the bar, not `shared read`. Covering
returns to the arbiter at the B3 gate only if, with the shipped index, the top cve or
advisory id on QA misses the 2.5 s line (rows / 16 x QA cold ms per row) or its range 1
or open range takes over 5 s, its heap-fetch share is at most 10% on three reads at least
1 h apart (one under writer load), and a covering arm is inside the stops. The census read
`EXPLAIN (ANALYZE, BUFFERS) SELECT count(*) FROM supply_chain_impact_finding_supports WHERE cve_id = '<id>'`
plans as an Index Only Scan on the shipped cve index and prints Heap Fetches (0 after a
VACUUM on a 300,000-row corpus).

### W-B: default `fastupdate` against off, 100,000-row windows (8M table; supporting)

Each window after `CHECKPOINT` with no checkpoint inside, default and off interleaved
three times, both GINs, the default window ending with `gin_clean_pending_list`:
2,927,735,615 / 2,799,385,896 / 2,242,879,049 B (default) against 2,705,492,259 /
2,750,007,457 / 2,431,355,465 B (off); medians 2,799 MB and 2,705 MB, ranges overlap.
Off is not above default there. The corpus has 400-row members spread evenly and no
clustering of one member in a transaction, so it is the weaker guide for hot ids; the
W-A cells above bind.

### QA census fields owed at the B3 gate

H1 on QA lists the top 20 ids by rows for `cve_id`, `advisory_id` and the foreign
member, and per read: `relallvisible / relpages`, `n_dead_tup` with `n_ins_since_vacuum`,
and the oldest `backend_xmin` age. A replica census adds keys and rows per member
list-length bucket (under 30, 30 to 1,500, over 1,500), which tells whether the S2
shared-member shape exists. H3 reads the first-touch range 1 and open range of the top id
of each arm.

### Gates not run

`agent-canon` fails in this worktree because the installed role manifest
(`.agents/roles.json`) is missing: the agent-kit installer was not run here.
`golden-corpus-filter-exhaustive` fails because the host `yq` is the Python flavour
and reads the PR inventory as an unsupported schema. Neither reads this diff.
`make pre-push` and `make pre-pr` belong to the coordinator.
