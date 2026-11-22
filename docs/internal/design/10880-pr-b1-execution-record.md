# PR-B1 execution record for #10880

Refs #10880. Companion to
[the supports table proof](10880-pr-b1-supports-table-proof.md) and
[the supports read theory proof](10880-pr-b1-supports-read-theory-proof.md),
which hold the measured design this record builds. Written 2026-11-22. B1 ships
the table, its maintainer, its backfill, and an old-writer fence. No reader
serves from it; PR-B2 switches the readers.

Built under the arbiter's binding ruling for B1 (separate supports contract,
payload kept verbatim, a reducer-owned backfill with an old-writer fence,
migration 336, the read-time operator probe, and the rank-filter order). Stacked
on PR-B0 (`drive/10880-pr-b`). Commits on `drive/10880-pr-b1`, each test-first:

1. **Migration 336.** `supply_chain_impact_finding_supports`: one row per live
   finding fact, primary key `fact_id`, generation foreign key with cascade,
   indexes `(scope_id, generation_id)`, `(repository_id)`, `(finding_id)`, a
   partial `(canonical_key)` index for the operator override probe, and a plain
   `(generation_id)` index for the foreign key's cascade delete. The same
   migration adds the backfill marker and cursor table, the old-writer dirty-scope
   table, and the fence triggers on `fact_records`. The checksum manifest, the
   bootstrap golden digest, the schema order list, and the repository-retirement
   census carry the tables.
2. **Same-transaction maintainer.** `supplychain/core` gets the optional
   `SupplyChainImpactSupportsMaintainer` interface. The writer calls it after the
   fact insert and after the retraction, inside the conflict-domain lock. The
   storage implementation derives every column from `fact_records` through the
   winners helpers and writes only when a column changed.
3. **Backfill, fence and health.** A reducer-owned, marker-gated sweep fills the
   table for facts written earlier; the fence marks scopes that writers without
   the maintainer touch and the sweep drains them; telemetry and the readiness
   predicate.
4. **Contract.** A supports contract version and pin map, separate from the
   winners contract.
5. **Equality and plan-shape proofs.** A test-only candidate list read over the
   table, with the ruled rank-filter order, and a guard that pins the plan shape.

## Decisions

All three forks are settled by the ruling.

- **Separate contract.** `SupplyChainImpactSupportsContractVersion` is 1 and the
  marker is `supply_chain_impact_finding_supports_v1`. The pin map covers the
  writer upsert, the retraction delete, the backfill chunk, the marker
  completion, the readiness predicate and the writer setting; a one-character
  change to any of them breaks the pin test, and a seeded-RED test proves it. The
  winners contract, its pin map and its pin test are untouched and green. A bump
  creates a new marker, so the backfill runs again and granted reads answer 503
  until it finishes. Bump it only when the rows for the same facts change.
- **Payload verbatim.** The support's payload is the fact payload, byte for byte,
  with no key stripped. Every mirror check in the live suite compares the payload
  text of each support with its fact, and the write-on-change guard covers the
  payload. An earlier build stripped the per-pass keys the winners payload drops;
  that was wrong (PR-C makes the supports table the only copy) and is gone.
- **Backfill in the reducer.** One session advisory lock elects one sweeper
  across replicas. It is not a migration: a migration runs in PreSync while old
  reducers still write finding facts, so its snapshot would miss them and nothing
  would fence them.

Findings from reading the code:

- **Who writes operator-scope finding facts.** The same impact writer. The only
  `BatchInsertVersionedFacts` call that writes the finding kind is
  `commitSupplyChainImpactRows` (`writer_retract.go`, found by searching every
  non-test `BatchInsertVersionedFacts`, `INSERT INTO fact_records` and
  `UPDATE fact_records` call site); the operator pass runs through it.
  `VulnerabilitySuppressionStore.Upsert` writes only `vulnerability.suppression`
  facts and enqueues projector work. So the maintainer covers the operator scope
  with no hook in the suppression store, and the fence covers it as well. A live
  test writes an operator-scope finding through the real writer and finds its
  support with the operator scope id and no dirty mark.
- **The table mirrors every live finding fact.** Derived-image and
  scanner-associated rows are included, because the scanner classification rows
  land from a separate flow after the finding fact: a write-time exclusion would
  race that flow. The static-authority guard stays on the read side. The equality
  test shows the guard is load-bearing: removing it fails 49 cases.
- **The generation foreign key needs its own `(generation_id)` index.** The
  cascade runs `DELETE FROM ONLY supports WHERE generation_id = $1` once per
  deleted generation, inside the retention and repository-retirement
  transactions. An earlier draft of this record said PostgreSQL 18 reaches those
  rows by B-tree skip scan on the `(scope_id, generation_id)` index and needs no
  index of its own. That was wrong. The review measured, and the scale shim
  confirms, that 18.6 reads the whole `(scope_id, generation_id)` index once per
  deleted generation (`Index Searches: 1` over every leaf page; a skip scan would
  show one search per scope; the arbiter reports the same on 17). At 300,000
  supports the probe touches 326 buffers and at 1,200,000 it touches 1,115, and a
  50-generation `DELETE FROM scope_generations` takes 10.8 ms and 39.3 ms of
  trigger time. With a plain `(generation_id)` index the probe reads 1 to 3
  index buffers at both sizes and the 50-generation cascade takes 2.1 ms and
  2.6 ms ([cascade scale note](../evidence/10880-pr-b1/cascade_scale.md)).
  Migration 336, still unshipped, therefore carries the fifth index. The
  plan-shape guard pins the cascade statement, prepared under a generic plan, to
  `supply_chain_impact_finding_supports_generation_idx` by name with
  `generation_id` in the Index Cond, and its seeded violation drops that index in
  a rolled-back transaction and expects the checker to reject the plan. The
  index is required on every server version.

## The old-writer fence

The maintainer keeps supports current only for writers that run it. During a
rolling upgrade, reducers of the previous release still write finding facts
without it, and the backfill's sweep may already have passed their scope.

- **Trigger.** `AFTER INSERT` and `AFTER UPDATE` on `fact_records` `FOR EACH
  ROW WHEN (fact_kind = 'reducer_supply_chain_impact_finding' AND
  current_setting('eshu.supply_chain_impact_supports_writer', true) IS DISTINCT
  FROM 'supports_v1')` upserts the scope into the dirty-scope table and bumps its
  version. The new impact writer sets that setting for its transaction
  (`BeginSupplyChainImpactTx`), so its rows never reach the trigger body. It is
  the pattern of migration 139.
- **Drain.** Each backfill step first drains the oldest dirty scope: for each
  generation of the scope that holds finding facts it re-reads chunk by chunk
  under that generation's conflict-domain lock, then deletes the mark only when
  its version is unchanged, so a mark that lands during the drain survives.
- **Marker.** The marker is set by one statement that rechecks the fence, so it
  stays unset while a scope is dirty.
- **Readiness.** `SupplyChainImpactSupportsReady` is true when the marker for
  this binary's version is complete, no marker for a higher version exists (a
  newer binary ran the sweep), and the dirty-scope table is empty. A missing table
  reads as an error, which a reader treats as not ready.

Trigger cost, measured on a private `postgres:18` with 8,000 rows per case, three
repetitions, the host at load about 24 (so wall time and small WAL differences are
noise, and I report what the data supports):

| Insert of 8,000 rows | WAL | Time |
| --- | --- | --- |
| Impact kind, trigger disabled | 17.5 to 27.1 MB | 314 to 614 ms |
| Impact kind, writer setting present (the new writer; the WHEN clause is false) | 17.6 to 25.0 MB | 328 to 657 ms |
| Impact kind, no setting (an old writer; the trigger fires) | 25.2 to 26.2 MB | 788 to 1,097 ms |
| Another kind, trigger disabled | 18.0 to 18.4 MB | 353 to 854 ms |
| Another kind, trigger enabled (the WHEN clause is false) | 21.4 to 22.2 MB | 405 to 481 ms |

The baseline spread (17.5 to 27.1 MB for identical work) comes from page state and
full-page writes after checkpoints, so the WHEN-false rows cannot be told from the
disabled-trigger rows on WAL: a row the WHEN clause rejects writes nothing. The
old-writer case is the one that costs: one dirty-row upsert per fact row, about 60
to 70 microseconds a row here, and about +45% WAL over the lowest baseline. It
runs only while an old reducer writes, then goes away. A 20,000-row run showed the
same ordering (50.9 MB and 1.6 s without the trigger, 59.6 MB and 5.5 s firing).

## Concurrency

Lock order inside the writer transaction: the (scope, generation) advisory lock,
then the fact rows, then the support rows (both in `fact_id` order), then the
winners dirty log, then the retraction. The resweep takes its own advisory lock
and writes only winners, so no cycle exists with the writer. The backfill and the
drain take the writer's own advisory lock, through the exported
`SupplyChainImpactConflictDomainLockQuery` and key function, so a chunk cannot
interleave with a pass: without that, a chunk that read a fact live could insert
a support after a pass's retraction deleted it, leaving an orphan a reader would
serve as a finding.

Live proofs on `postgres:18`, enrolled in the reducer-contention gate and the
live-tests ledger:

| Test | What it shows | Mutation that turns it red |
| --- | --- | --- |
| `TestSupplyChainImpactSupportsFollowTheWriterLive` | One support per live fact after every pass, payload text equal to the fact's, columns equal the winner built from the same fact; replay under the same intent id leaves the `xmin` sum unchanged; replace set, partial pass, revival, FK cascade. | Retraction delete never matches; a stripped payload key (red before the payload fix). |
| `TestSupplyChainImpactSupportsSurviveRacingWritersLive` | Eight writers over two scopes, five rounds, with a concurrent winners resweep: no error beyond the retryable superseded pass, no deadlock, supports equal live facts after every round. | None run. |
| `TestSupplyChainImpactSupportsBackfillLive` | Fills with two-fact chunks, removes an orphan an old writer left, withholds the marker until the sweep and verification finish, no-op after completion. | None run. |
| `TestSupplyChainImpactSupportsBackfillRestartsOnAMissLive` | A support that goes missing after the sweep passed it makes verification restart the sweep. | Skip the restart. |
| `TestSupplyChainImpactSupportsBackfillRacesWritersLive` | One-row chunks racing real writers: no orphan, no missing support, no deadlock. | None run. |
| `TestSupplyChainImpactSupportsBackfillWaitsForTheConflictDomainLockLive` | A step waits for a held pair lock and runs after release. | Remove the lock from the chunk. |
| `TestSupplyChainImpactSupportsOldWriterFenceLive` | An old writer's insert and tombstone update mark the scope and the table not ready; the backfill drains it and the table is ready; a setting-carrying write, another kind, and a real writer pass mark nothing. | Drop the writer setting from `BeginSupplyChainImpactTx`. |
| `TestSupplyChainImpactSupportsMarkerWaitsForTheFenceLive` | The marker completes only with an empty fence. | None run. |
| `TestSupplyChainImpactSupportsFreshInstallMarksImmediatelyLive` | An empty database is ready within two steps. | None run. |
| `TestSupplyChainImpactSupportsResumeAfterKillLive` | A new backfill value resumes from the stored cursor; a replayed full sweep rewrites no support (`xmin` sum unchanged). | None run. |
| `TestSupplyChainImpactSupportsMarkerAboveTheBinaryIsNotReadyLive` | A marker for a higher version reads as not ready. | None run. |
| `TestSupplyChainImpactSupportsOperatorScopeWriteLive` | An operator-scope finding written by the real writer lands in supports with the operator scope id and marks nothing dirty. | None run. |

Hermetic tests pin the supports contract hash, the seeded-RED pin change, the
marker name against the version, the transaction setting being the writer's first
statement, the readiness predicate (including a missing table), and the commit-time
telemetry counter.

## Performance evidence

Performance Evidence: measured on a private `postgres:18` (default settings) with
the harnesses in [the evidence directory](../evidence/10880-pr-b1/README.md), host
load average about 24 on 16 cores, so wall times are noisy and WAL bytes are the
stable figure.

**Writer write cost** (the real writer, 3,000 findings in one scope, three
repetitions; the comparison forwards every optional transaction surface but the
supports maintainer; payload verbatim):

| Pass | WAL with supports | WAL without | Delta |
| --- | --- | --- | --- |
| Insert 3,000 findings | 24.85 MB | 17.64 to 17.67 MB | +40.6% to +40.9% |
| Replay the same 3,000 under a new intent id | 21.22 MB | 13.76 to 13.82 MB | +53.5% to +54.2% |
| Retract 1,500 of 3,000 (1,500 kept under a new intent id) | 14.60 MB | 10.87 to 11.07 MB | +31.8% to +34.3% |

These are synthetic and differ from the +19.4% (insert with retraction) the ruling
reasoned on, which measured the support statements alone and without the real
writer's dirty-log and pass-state work. The reasons, from the runs: a verbatim
payload makes every support carry the fact's per-pass keys, so a replay that
carries a new intent id rewrites the support as it rewrites the fact (an earlier
build stripped those keys and measured +0.4% to +1.6% on replay and retraction, at
the cost of the data PR-C needs); and the payload column is about half of the
insert delta (an earlier run with the payload set to an empty object gave +20.7% to
+22.2%). The ruling makes the +25% impact-WAL rule a QA before/after window check
before B3, not a B1 gate, and these numbers go to that review as a measured
synthetic fact.

**QA window for the before/after check.** Report, for equal windows before and
after the roll: `pg_stat_wal.wal_bytes`, the table size, and the impact-write WAL
per row written. Add, from a committed queue SQL, the supply-chain reducer items
(succeeded and reopened) and the impact WAL per reopened item and per row written:
reopens drive fact-side rewrites (the fencing-only `BatchInsertVersionedFacts`,
`writer_retract.go:166`; the arbiter's note for PR-C counts 304 of 328 items in
seven days as reopens), so a verbatim support rewritten on each reopen is the
number that decides whether the +25% rule holds. PR-C's net-negative WAL must be
measured the same way, not credited from an upper bound.

**Backfill duration** (100,000 facts in 2,000 scopes, 500-fact chunks, one
chunk per scope here because each scope holds 50 facts; the final shape, verbatim
payload, measured at the merged head with the drain loop in place): 4,001 steps
(two per scope: one writes the chunk, the next finds the pair exhausted and moves
on), 41.8 s, 2,394 supports per second, longest step 276 ms (host load about 27).
An earlier build with the stripped payload measured 30.3 s and 3,297 supports per
second, so the wider row costs about 27% of the rate. A production scope with many
thousands of facts runs 500-fact chunks of about 200 ms each; a writer waits at
most one chunk. At this rate 5 million supports take about 35 minutes. The walk
never sorts the fact table: pair discovery is an index range scan (0.03 ms in
`EXPLAIN ANALYZE` at 100,000 facts), and the per-chunk sort is bounded by one
pair's live findings. A first version ordered the whole table by `(scope_id,
generation_id, fact_id)` per step and timed out at 100,000 facts; the scale
harness found it, and the walk was rewritten before commit.

**Read blocks** (legacy list read against the committed candidate at the earlier
payload shape, 100,000 facts, status filter, buffer hits plus reads of one
`EXPLAIN (ANALYZE, BUFFERS)` run; block accesses bound distinct blocks from above):

| Grant | Legacy blocks | Candidate blocks | Ratio |
| --- | --- | --- | --- |
| 5 scopes | 304,771 | 2,755 | 0.009 |
| 80 scopes | 312,271 | 40,985 | 0.131 |
| 400 scopes | 344,271 | 203,744 | 0.592 |
| 20 repositories | 306,271 | 10,415 | 0.034 |

The candidate stays far inside the +10% bound the ruling set for cold reads. The
candidate's rank-filter order has since changed to the ruled pick-then-filter
order, which filters after the pick; the block counts above predate that change and
are NOT_CHECKED at the final shape.

## Equality and the rank-filter order

The test-only candidate is derived from the shipped `ListFindingsQuery` by marker
surgery, so the legacy read cannot drift from it unseen. It reads the supports in
two grant arms, applies the active-generation join afterward, keeps the
static-authority guard, and uses an indexed lateral probe for the operator override
(order by priority score, payload finding id, fact id, limit 1; the operator
scope's active generation; non-tombstone; non-active state; the same clock bind).

- **Non-rank filters** (status, image ref, profile, CVE, advisory, suppression
  state, include-suppressed, paging) are byte-equal to the legacy read across 11
  grant shapes.
- **Rank filters** (severity, priority bucket, minimum priority) follow the ruled
  order: pick among the granted supports that pass the key and existence filters,
  apply the rank filter to the picked row, then the suppression filters. The legacy
  read filters first and picks after, so the 154-case legacy matrix does not prove
  this rule. These cases compare with an independent oracle (the legacy read with
  the rank binds empty, unpaged, the picked rows filtered in Go).
  Nine rank cases differ from legacy; they are pinned in the test as the list PR-B3
  changes on purpose. Example: a grant of scopes SA and SB with
  `priority_bucket=low` returns `m1` (priority 40) and `m2` (priority 30) from
  legacy, because their low-priority copy in SA passes the filter before the pick;
  the ruled read picks the SB copy of `m1` (priority 80) and the SA copy of `m2`
  (priority 90) first, and drops both.
- **Operator overlay cases:** live, expired, an expiry the cast cannot read, two
  competing overrides for one key, an overlay in an operator generation a later
  generation superseded, a grant naming the operator scope, and every grant shape.
- **Teeth.** Removing the static-authority guard fails 49 cases and removing the
  override probe fails 61 (measured on the first matrix; the guard and probe
  are unchanged).
- **Plan shape.** At the 400-scope shape over 144,000 supports in 2,400 scopes the
  plan needs both grant indexes and no sequential scan; the natural single-WHERE
  form fails the same checker; the cascade delete uses the `(generation_id)`
  index by name, and the same checker rejects it with that index dropped.
- The HTTP reference text for B2: "the canonical copy among the copies you may see
  that match the other filters". Filters the ruling does not classify
  (`impact_status`, `subject_digest`, `repository_id`, runtime filters) keep
  existence semantics here; how they diverge on QA is a census for B2.

## Observability evidence

Observability Evidence: `eshu_dp_supply_chain_impact_supports_rows_total`
(`action`, `source`), `..._backfill_steps_total` (`outcome`),
`..._backfill_step_duration_seconds`, `..._dirty_scopes`,
`..._backfill_last_success_timestamp_seconds` and `..._backfill_ready`, documented
in [Supply-Chain Impact Supports](../../public/reference/telemetry/supply-chain-impact-supports.md)
and the reducer-domain coverage row. Writer rows count after commit; a hermetic
test pins that a rollback and a failed commit count nothing. At 3 AM an operator
sees a backfill that never finishes (gauge stays 0, steps are `contended` or
`failed`, last success stops), a sweep restarting (`verify_gap` rising), a
maintainer that stopped (`writer` inserted and updated flat while findings are
written), or an old writer still running (the dirty-scope gauge stays above zero).

## Pre-merge conditions and rollout

- **QA census before B1 merges.** Run the census query in the "QA census query"
  paragraph of [the proof record](10880-pr-b1-supports-read-theory-proof.md)
  against the QA read replica, and paste its six figures in the PR body. It
  sizes the table and shows whether supports of one key differ on profile or
  image ref, which decides the existence filters in B2.
- **After deploy, before any B2 reader ships:** the ready gauge reads 1, the dirty
  gauge reads 0, and this returns zero:

  ```sql
  SELECT count(*) FROM fact_records AS f
  WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
    AND NOT EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports AS s WHERE s.fact_id = f.fact_id);
  ```

- **B3 precondition on QA:** the supports v1 marker is recorded and the fence is
  empty.
- **Single QA pin.** The supports contract does not raise the winners contract,
  so B1 does not need its own winners full; it rides the agreed QA image pin if
  that is ready, or any later agreed pin, and its roll must record no second
  winners full. Whether the winners v2 build is on QA is NOT_CHECKED here.
- **B2 reader contract.** A grant-scoped call answers 503 when
  `SupplyChainImpactSupportsReady` is false, errors, or sees a stored marker above
  the binary's version, and is never served from winners as-is.

## NOT_CHECKED

- A production-scale backfill (the figure above is 100,000 facts).
- Read blocks and timings of the candidate at the ruled rank-filter order and the
  verbatim payload.
- A cold-start run with the page cache dropped: block accesses are reported, not
  the distinct blocks of a restarted server.
- The trigger's WHEN-false cost at the CPU level; only WAL and wall time under
  load were measured.
- A PostgreSQL other than 18.6 (the cascade scale note is 18.6 only; the index is
  required whichever planner release runs).
- The keyed resweep's own read of the table: B1 adds no such reader.
- Whether winners v2 is on QA, and the QA impact-WAL window.
- `make pre-push`, `make pre-pr`, and CI: left to the coordinator by order.
</content>
