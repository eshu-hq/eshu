# PR-B1 execution record for #10880

Refs #10880. Companion to
[the supports table proof](10880-pr-b1-supports-table-proof.md) and
[the supports read theory proof](10880-pr-b1-supports-read-theory-proof.md),
which hold the measured design this record builds. Written 2026-11-22. B1 ships
the table, its maintainer, and its backfill. No reader serves from it; PR-B2
switches the readers.

Stacked on PR-B0 (`drive/10880-pr-b`, head `76b9168a90` when this was written).
Commits on `drive/10880-pr-b1`, each test-first:

1. **Migration 336.** `supply_chain_impact_finding_supports`: one row per live
   finding fact, primary key `fact_id`, generation foreign key with cascade,
   indexes `(scope_id, generation_id)`, `(repository_id)`, `(finding_id)` and a
   partial `(canonical_key)` index for the operator override probe. The same
   migration adds `supply_chain_impact_finding_supports_backfill`, the marker and
   cursor row. The checksum manifest, the bootstrap golden count and digest, the
   schema order list, and the repository-retirement census carry both tables.
2. **Same-transaction maintainer.** `supplychain/core` gets the optional
   `SupplyChainImpactSupportsMaintainer` interface. The writer calls it after the
   fact insert and after the retraction, inside the conflict-domain lock. The
   storage implementation derives every column from `fact_records` through the
   winners helpers, so a support cannot drift from a winner built from the same
   fact, and writes only when a column changed.
3. **Backfill and health.** A reducer-owned, marker-gated sweep fills the table
   for facts written earlier, plus telemetry and the readiness predicate.
4. **Equality and plan-shape proofs.** A test-only candidate list read over the
   table equals the legacy read across 160 grant and filter cases, and a guard
   pins the plan shape.

## Decisions taken

The three forks I left for the arbiter were settled by precedent so the build
could go on. Each is cheap to reverse before B2.

- **Separate contract.** The supports table has its own marker table and
  `SupplyChainImpactSupportsContractVersion`. It does not bump
  `SupplyChainImpactWinnersContractVersion`, because the winners contract pins
  what a winners row means and the supports table does not change that.
- **Payload on the support row.** Kept, stripped of the per-pass fields the
  winners payload also drops (`intent_id`, `cause`, `generation_id`,
  `provenance.content_digest`). Without the strip, a replay of an unchanged
  finding carries a new intent id and rewrites every support. See the write cost
  below for what the payload costs.
- **Backfill placement.** A reducer loop, as the package manifest backfill does,
  with a cluster-wide session advisory lock electing one sweeper.

Two facts from reading the code settle questions the plan left open:

- **Operator overlay needs no extra write path.** Operator-scope finding facts
  are written by the ordinary impact writer pass for the operator scope (the
  fan-out tests call it "the operator pass"), so the maintainer already covers
  them. `VulnerabilitySuppressionStore.Upsert` writes only
  `vulnerability.suppression` facts, which are not findings.
- **The table mirrors every live finding fact.** Derived-image and
  scanner-associated rows are included. The static-authority guard stays on the
  read side because the scanner classification rows land from a separate flow
  after the finding fact; a write-time exclusion would race it. The equality test
  shows the guard is load-bearing: removing it fails 49 cases.

## Concurrency

Lock order inside the writer transaction: the (scope, generation) advisory lock,
then the fact rows, then the support rows (both in `fact_id` order), then the
winners dirty log, then the retraction. The resweep takes its own advisory lock
and writes only winners, so no cycle exists with the writer. The backfill takes
the writer's own advisory lock, through the now-exported
`SupplyChainImpactConflictDomainLockQuery` and key function, so a chunk cannot
interleave with a pass: without that, a chunk that read a fact live could insert
a support after a pass's retraction deleted it, leaving an orphan a reader would
serve as a finding.

Live proofs on `postgres:18`, enrolled in the reducer-contention gate and the
live-tests ledger:

| Test | What it shows | Mutation that turns it red |
| --- | --- | --- |
| `TestSupplyChainImpactSupportsFollowTheWriterLive` | One support per live fact after every pass; columns equal the winner built from the same fact; replay leaves the `xmin` sum unchanged; replace set, partial pass, revival and the FK cascade. | Retraction delete never matches. |
| `TestSupplyChainImpactSupportsSurviveRacingWritersLive` | Eight writers over two scopes, five rounds, with a concurrent winners resweep: no error beyond the retryable superseded pass, no deadlock, supports equal live facts after every round. | None run. |
| `TestSupplyChainImpactSupportsBackfillLive` | Fills with two-fact chunks, removes an orphan an old writer left, withholds the marker until the sweep and verification finish, no-op after completion. | None run. |
| `TestSupplyChainImpactSupportsBackfillRestartsOnAMissLive` | A support that goes missing after the sweep passed it makes verification restart the sweep; the marker is set only after the next sweep fills it. | Skip the restart. |
| `TestSupplyChainImpactSupportsBackfillRacesWritersLive` | One-row chunks racing real writers: no orphan, no missing support, no deadlock. | None run. |
| `TestSupplyChainImpactSupportsBackfillWaitsForTheConflictDomainLockLive` | A step waits for a held pair lock and runs after release. | Remove the lock from the chunk. |

## Performance evidence

Performance Evidence: measured on a private `postgres:18` (default settings)
with the harnesses in
[the evidence directory](../evidence/10880-pr-b1/README.md), host load average
about 21 on 16 cores, so wall times are noisy and WAL bytes are the stable
figure.

**Writer write cost** (the real writer, 3,000 findings in one scope, three
repetitions; the comparison forwards every optional transaction surface but the
supports maintainer):

| Pass | WAL with supports | WAL without | Delta |
| --- | --- | --- | --- |
| Insert 3,000 findings | 25.82 MB | 17.64 to 17.85 MB | +44.6% to +46.4% |
| Replay the same 3,000 | 13.93 MB | 13.76 to 14.16 MB | -1.6% to +1.2% |
| Retract 1,500 of 3,000 | 11.04 MB | 10.87 to 11.03 MB | +0.1% to +1.6% |

The payload column accounts for about 24 points of the insert cost: with the
payload column set to an empty object the insert delta is +20.7% to +22.2%. The
second proof measured +19.4% on the statements alone, without the real writer's
dirty-log and pass-state work, which is why the number moved. The steady state is
replays and small changes, which cost nothing: write-on-change holds. The insert
cost is the one a first scan and a new advisory pay. I set a working threshold of
+25% WAL on insert and did not state it before measuring; the payload column
crosses it. Whether to keep the payload on the support row until the fact
payloads retire (slice 3 frees the ten dedicated fact indexes and the fact
payload, 266 MB in the proof corpus) or to read page payloads from the fact row
for now is the arbiter's call, so I left the payload in and report the cost.

**Backfill duration** (100,000 facts in 2,000 scopes, 500-fact chunks, one
transaction per scope here because each scope holds 50 facts): 2,001 steps,
30.3 s, 3,297 supports per second, longest step 2.04 s under load. A production
scope with many thousands of facts runs 500-fact chunks of about 150 ms each; a
writer waits at most one chunk. At this rate 5 million supports take about 25
minutes. The walk never sorts the fact table: pair discovery is an index range
scan (0.03 ms in `EXPLAIN ANALYZE` at 100,000 facts), and the per-chunk sort is
bounded by one pair's live findings. A first version ordered the whole table by
`(scope_id, generation_id, fact_id)` per step and timed out at 100,000 facts;
the scale harness found it, and the walk was rewritten before commit.

**Read blocks** (legacy list read against the committed candidate, 100,000 facts,
status filter, buffer hits plus reads of one `EXPLAIN (ANALYZE, BUFFERS)` run;
block accesses bound distinct blocks from above):

| Grant | Legacy blocks | Candidate blocks | Ratio | Legacy ms | Candidate ms |
| --- | --- | --- | --- | --- | --- |
| 5 scopes | 304,771 | 2,755 | 0.009 | 139 to 175 | 3.1 to 42.8 |
| 80 scopes | 312,271 | 40,985 | 0.131 | 159 to 161 | 43 to 70 |
| 400 scopes | 344,271 | 203,744 | 0.592 | 299 to 396 | 249 to 251 |
| 20 repositories | 306,271 | 10,415 | 0.034 | 142 to 153 | 10.6 to 10.7 |

The profile=precise filter gave identical block counts. The candidate stays far
inside the +10% bound the ruling set for cold reads. The 400-scope grant reads
the most because it reads the most supports (20,000 of 100,000).

## Observability evidence

Observability Evidence: `eshu_dp_supply_chain_impact_supports_rows_total`
(`action`, `source`), `eshu_dp_supply_chain_impact_supports_backfill_steps_total`
(`outcome`), and `eshu_dp_supply_chain_impact_supports_backfill_ready`, documented
in [Supply-Chain Impact Supports](../../public/reference/telemetry/supply-chain-impact-supports.md)
and the reducer-domain coverage row. Writer rows count after commit; a hermetic
test pins that a rollback and a failed commit count nothing. At 3 AM an operator
sees a backfill that never finishes (gauge stays 0, steps are `contended` or
`failed`), a sweep restarting (`verify_gap` rising), or a maintainer that
stopped (`writer` inserted and updated flat while findings are written).

## Pre-merge conditions and rollout

- **QA census before B1 merges.** Run the census query in the "QA census query"
  paragraph of [the proof record](10880-pr-b1-supports-read-theory-proof.md)
  against the QA read replica, and paste its six figures in the PR body. It
  sizes the table and shows whether supports of one key differ on profile or
  image ref, which decides the existence filters in B2.
- **After deploy, before any B2 reader ships:** the gauge reads 1, and this
  returns zero:

  ```sql
  SELECT count(*) FROM fact_records AS f
  WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
    AND NOT EXISTS (SELECT 1 FROM supply_chain_impact_finding_supports AS s WHERE s.fact_id = f.fact_id);
  ```

- **Rolling upgrade.** A writer without the maintainer that runs after the sweep
  passed its pair leaves a gap or an orphan. The marker is not re-verified once
  set, so B2 readers must treat the marker as a deploy-time readiness fact and
  the QA check above as the gate; a periodic re-verification is a follow-up if
  the rollout shows mixed-version windows are long.
- **B2 reader contract.** A grant-scoped call answers 503 when
  `SupplyChainImpactSupportsReady` is false or errors (a missing table reads as
  an error), and is never served from winners as-is.

## NOT_CHECKED

- A production-scale backfill (the figures above are 100,000 facts).
- A cold-start run with the page cache dropped: block accesses are reported, not
  the distinct blocks of a restarted server.
- The keyed resweep's own read of the table: B1 adds no such reader.
- `make pre-push`, `make pre-pr`, and the registry static gates: left to the
  coordinator by order.
