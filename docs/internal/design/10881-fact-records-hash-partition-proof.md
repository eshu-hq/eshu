# fact_records HASH(scope_id) partition: theory proof and design record (#10881)

Status: **Proposed**. P1 theory proof (small-scale, disposable PG18) and
this P2 record are done. No implementation, no migration: the gate is
#10880 (OPEN, verified 2026-11-21) plus the P3 reads-off inventory and the
P5 representative-scale rehearsal. A measured NO-GO closes the issue.

Refs #10881. Part of #10860 (phase 3, step 2). Blocked by #10880 (reads
move off first) and answered by #10862 (CLOSED; baseline below). Authority:
#10861 D6 replacement plus arbiter ruling 2026-11-18
(`docs/internal/arbiter/10861-reducer-execution-model-ruling-2026-11-18.md`,
D6 lines 66-74 reject-as-written, 116-131 replacement).

The issue title ("by repository") is retired: the key is `scope_id`.
Non-repository scopes have no repository key, and the cited precedent is
`PARTITION BY HASH (scope_id)` on `eshu_search_index_terms`
(`go/internal/storage/postgres/migrations/003b_eshu_search_index.sql`).
The queue (`fact_work_items`) is NOT divided by this issue; see Decision 6.

## Decision 1: partition key, count, and identity

- Key: `HASH(scope_id)`. Modulus candidates 32 and 64, both built and
  measured below. No choice is final until the P5 rehearsal at
  representative scale; modulus cannot change without a full rebuild.
- Primary key `fact_id` becomes `(scope_id, fact_id)`. A partitioned unique
  index must include the partition key. This changes dedup semantics (see
  Decision 2), so the PK change is the real risk, not the partitioning.
- The precedent (64 partitions on the search-terms table) is not copied
  blindly: per-partition size, planner overhead, and index multiplication
  are measured for both moduli below.

## Decision 2: identity change and the three censuses

`ON CONFLICT (fact_id)` upserts become `ON CONFLICT (scope_id, fact_id)`
in the same PR as any DDL. Proof on the disposable copy:

- Undivided PK rejects `fact_id='fact-1'` under a second scope
  (`duplicate key violates "fact_undivided_pkey"`); the divided PK accepts
  it (row visible under both `scope-1` and `scope-X`), then the probe row
  was deleted to keep the corpora identical.
- Dedup-census query shape (decision 3a gate): `SELECT fact_id ...
  GROUP BY fact_id HAVING count(DISTINCT scope_id) > 1` returns 0 on the
  synthetic corpus. On real data this MUST return 0 before any migration
  PR; any value above zero changes dedup semantics and stops the design
  for a fresh ruling. NOT measured on real data in this phase.

Writer census: the sites that write `fact_records` and so must move to
`ON CONFLICT (scope_id, fact_id)` are the five production `INSERT INTO
fact_records` statements. Derive them, do not copy a count:
`bash scripts/dev/10881-partition/census_code.sh` (first section). At the
commit this record cites they are:

| Site | Kind |
| --- | --- |
| `go/internal/storage/postgres/facts_streaming.go:186` | collector commit upsert (fenced) |
| `go/internal/reducer/factwrite/single_insert.go:33` | reducer single insert (no fence guard) |
| `go/internal/reducer/factwrite/batch_insert.go:189` | reducer batch insert (fenced) |
| `go/internal/reducer/factwrite/batch_insert_versioned.go:130` | reducer versioned batch insert (fenced) |
| `go/internal/reducer/eshusearch/eshu_search_document_writer_queries.go:103` | search document writer (no fence guard) |

An earlier draft of this record said "seven sites". That was wrong by two:
`relationship_reference_keys.go:404` and migration
`058_relationship_reference_candidate_keys.sql:48` upsert
`relationship_reference_candidate_keys` against its own primary key and do
not move. Test-only and fixture writers (`testfixtures/*`, the bench
seeders in `cmd/read-api-latency-gate`) move with the same PR.

Reader census: derive the join count with
`git grep -n 'JOIN fact_records' -- 'go/*.go' ':!*_test.go' | wc -l`
(and `git grep -l ... | wc -l` for files); `census_code.sh` prints both.
The earlier draft's "66 lines across 40 non-test files" does not reproduce:
the Go figure at the cited commit is the command's output (60 lines in 34
files when this record was corrected). The fact_id-only lookups (a join or
`= ANY` on `fact_id` with no `scope_id` beside it) are the second section of
`census_code.sh`; the per-site disposition is in the measurement record
(`10881-fact-records-hash-partition-measurements.md`).

FK census: three tables reference `fact_records(fact_id)`: migration 058
(`relationship_reference_candidate_keys`), 139
(`package_manifest_consumption_keys`), 143
(`package_registry_identity_keys`). All three have drop migrations, and
`TestBootstrapDropsEveryFactRecordsForeignKeyLive` asserts zero FKs
reference `fact_records` after bootstrap. The partition migration
therefore requires the drops to have run; no new FK may reference the
partitioned PK by `fact_id` alone.

## Decision 3: P1 measurements (disposable PG18, 155,000-row corpus)

Corpus: 200 scopes x 500 facts plus one giant scope of 55,000 rows
(35.5% of the table, mirroring the cited vulnerability-intelligence
skew). Same rows in all three shapes; `VACUUM ANALYZE` before measuring.

| # | Divided (32 / 64) vs undivided | Result |
| --- | --- | --- |
| Pruning | Scope-bounded select touches exactly 1 partition (`fact_div32_p5`, `fact_div64_p37`); heap blocks 38 / 17 vs 500 | Pruning works; narrower per-partition indexes |
| Plan cost | Execution 0.146ms / 0.126ms vs 0.852ms at this scale | Directional only; production numbers need P5 |
| Unscoped join | fact_id-alone self-join fans out over all 32 partitions (Append over p0..p31) vs one scan undivided | Confirms the A4 inventory obligation |
| Skew | Giant scope lands wholly in one partition (p3: 58,500 of 155,000; giant share of its partition 35.5%) | Hash does not split one scope; largest-share recorded |
| Indexes | 2 indexes undivided; 66 total at modulus 32 (x33); 130 total at modulus 64 (x65) | Tenant-note input (Decision 5) |
| Removal | Scope-keyed DELETE prunes to one partition (buffers 578 vs 1,502); DELETE+VACUUM shape identical both ways, no DETACH benefit | HASH gives no TRUNCATE; P5 must show proportional cost at scale |

The small-scale removal timing (tens of ms both ways) claims nothing
about 415GB. It proves the mechanism (pruned delete, same vacuum shape),
not the benefit. The benefit is unproven until P5.

## Decision 4: kill criteria (a measured NO-GO closes the issue)

- Dedup census above zero on real data: stop, re-rule.
- Divided copy not materially better than undivided on retired-snapshot
  DELETE+VACUUM-to-steady-state at representative scale: NO-GO, no
  415GB rebuild for zero gain.
- Five busiest fact-load plans regress beyond the #10862 tolerance on the
  divided copy: NO-GO or fix-first. Baseline anchors: QA 9,391 scopes,
  83.7M live facts, 1,094GB; claim p50 0.011s p95 0.013s; replay
  flatness rule at most 1.10x (`reducer_queue_10862_replay_test.go`).
  The #10862 baseline doc was deleted under #10990, so per-query
  thresholds are re-pinned from the replay test before any migration PR.
- #10880 not landed or reads-off inventory not clean: no DDL, no branch.

## Decision 5: migration shape and tenant notes (only if gates pass)

New HASH-partitioned table, backfill, verified cutover; AccessExclusive
only at swap; old table retained until post-cutover verification passes.
Way back is rename-back / roll-forward. No downgrade-migration rehearsal.
Rehearse end to end at representative scale with duration, max lock hold,
WAL/replication impact, disk headroom, and rollback steps recorded.
Shared-DB tenant notes: autovacuum worker load times partitions, total
index count times modulus (x33 / x65 measured), planner time rises for
unscoped queries, and one tenant's giant scope still lands in one
partition (35.5% share measured).

## Decision 6: the queue stays whole

No claim-fairness proof exists for dividing `fact_work_items`, and D6
expressly excludes it. This issue leaves the queue in one piece. Any
future queue split needs its own issue with a competing-workers fairness
proof on the built binary against the largest realistic backlog: no lost
wake, no double claim, no per-partition claim serialization, no small-
scope starvation, save-transaction latency within baseline tolerance.

## Acceptance mapping (arbiter A1-A7)

- A1 key/count/id-rule: recorded above; collision count 0 on synthetic
  corpus, NOT_CHECKED on real data (P1 executor item).
- A2 removal: mechanism proven, benefit unproven; P5 rehearsal required.
- A3 five busiest plans: tolerance anchors recorded; per-query re-pin
  required before any migration PR.
- A4 cross-scope inventory: derived by `census_code.sh`; per-site
  disposition is in the measurement record. Execution of the reader
  moves still waits on #10880 (P3 gate).
- A5 migration proof: shape and way back recorded; rehearsal is P5.
- A6 tenant notes: index multiplication and giant-scope share measured.
- A7 queue: left whole, with the future-proof bar stated in Decision 6.
