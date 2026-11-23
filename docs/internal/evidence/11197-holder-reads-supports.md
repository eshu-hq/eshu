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

## Writer WAL cost of the four indexes (the real writer)

Harness: the B1 write-cost harness shape on `PostgresSupplyChainImpactWriter`
(3,000 findings: insert, replay under a new intent id, retract 1,500), fresh
isolated database per run, three repetitions per cell, `pg_stat_wal` deltas
(records, full-page images, bytes) of the writer's whole WAL, the GIN pending list
flushed inside every measured pass. Indexes added one at a time, cumulatively, on
top of B1-shipped. Cold = `CHECKPOINT` before each pass; warm = after a priming
insert, replay and retract, no checkpoint. S1 is the B1 shape (no group members);
S2 adds six group members per finding (two shared, one hot foreign member).

| Pass (S2) | B1-shipped bytes | +cve | +advisory | +members | +tokens (final set) |
| --- | --- | --- | --- | --- | --- |
| Cold insert | 27,129,085 | +2.6% | +5.0% | +15.3% | +27.4% (34,576,011; 117,121 records, 506 FPI) |
| Cold replay | 39,923,909 | +3.1% | +4.2% | +11.7% | +18.8% (47,427,865; 119,443 records, 2,968 FPI) |
| Warm insert | 31,786,868 | +12.6% (13% spread) | +5.5% | +12.0% | +19.8% (38,070,548; 134,833 records, 70 FPI) |
| Warm replay | 22,864,751 | +2.9% | +5.6% | +14.9% | +26.4% (28,906,054; 116,662 records, 65 FPI) |
| Cold / warm retract, final set | 44,546,040 / 18,971,020 | | | | +4.4% / -1.7% (spreads 22% and 0%) |

S1 (no group members): cold insert +6.6%, cold replay +15.7% (25% spread), warm
insert +4.6%, warm replay +7.7%. The arbiter's stop lines were +30 points for
insert and +36 for replay; the worst cells are S2 cold insert +27.4 and S2 warm
replay +26.4. This is **above the epic's +25% impact-WAL rule** on the insert and
replay passes of S2, and the two GINs carry most of it. The earlier +15.7% figure
was one S1 cell. Index sizes per 3,000 findings (S2): cve 0.93 MB, advisory
0.72 MB, members 1.43 MB, tokens 1.75 MB.

HOT updates: the replay pass wrote `n_tup_upd` 9,000 (3 reps of 3,000) with
`n_tup_hot_upd` 0 in every variant, B1-shipped included: the supports rewrite is
not HOT today (the payload changes and the page is full), so the GINs on the
payload cost no HOT share and no stored-column variant is needed.

The only stop for the supports writes is an image rollback; there is no runtime
switch.

## Readiness

Every read answers only from a ready supports table: the backfill marker for the
binary's contract version complete, no marker for a newer version, and an empty
old-writer fence (`supplyChainImpactSupportsReadySQL`, contract-pinned). The
check is the shared B2c commit's `RequireSupplyChainImpactSupportsReady`; it runs
as the first statement of every moved read's transaction, in the same snapshot as
the read, so a table that goes not-ready cannot be read half-way. A table that is
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
outcome `served` or `not_ready`). A sustained `not_ready` rate means the backfill
is unfinished or an old-writer image is still running.

## Plans after the move (final indexes, VACUUM ANALYZE)

Disposable `postgres:18`, bootstrap schema with migrations 339 to 342, 100,000
impact facts across 2,000 scopes (hot CVE at 10% of rows, a hot foreign group member,
superseded generations), supports filled by the real backfill, then
`VACUUM (ANALYZE)` on both tables (a GIN pending list left by the backfill makes the
planner price the members arm at a seq scan, so the flush is part of the setup, as
autovacuum is in production). `fact_records` 156 MB with 218 MB of indexes;
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
| Holder reads, the real writer, a GIN pending-list flush and a cascade-deleting scope run together with no read error, no deadlock, no torn replacement | `TestSupplyChainImpactSupportsHolderReadsUnderWriterContentionLive` |
| The fence checks readiness before it detaches, and keeps the #11147 reopen order | `TestFenceStopsBeforeDetachingWhenSupportsAreNotReady`, `TestFenceReopensHoldersInWorkItemLockOrder`, `TestFenceReopensHoldersInDeadLetterLockOrderLive` |
| The typed refusal reaches the queue unwrapped | `core/fanout_supports_not_ready_test.go` |

Performance Evidence: the plans table above (supports buffers at or below the fact
statements on every moved read, final indexes, `VACUUM (ANALYZE)`) and the WAL table
(S2 cold insert +27.4% and warm replay +26.4% over B1-shipped, inside the arbiter's +30
and +36 stop lines; above the epic's +25% impact-WAL rule, which this note names).

No-Regression Evidence: no fan-out, queue, lease or lock path changes: the holder
statements keep their shape and their chunking, the fence keeps the #11147 reopen
order, and the readiness check is one read of the marker and dirty tables per
transaction.

Observability Evidence: `eshu_dp_supply_chain_impact_supports_reads_total` (`source`
carries the seven new readers; `outcome` `served` or `not_ready`), the existing
`eshu_dp_supply_chain_impact_supports_backfill_ready` gauge and the shared
not-ready failure class `supply_chain_impact_supports_not_ready`; the fence emits the
same counter with `source=go_modules_fence`.
