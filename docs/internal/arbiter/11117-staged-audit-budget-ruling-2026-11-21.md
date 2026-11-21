# #11117 staged-audit budget ruling (2026-11-21)

Arbiter ruling (arbiter-eshu, binding) on the staged winners audit's corpus
budget at QA scale. File citations are at origin/main `3d9ab0b5cf` and the A
head the arbiter read (`abd0d83c34`); line numbers are omitted because the
code has moved since.

Recorded verbatim by the coordinator from arbiter-staged-audit-budget (the arbiter role is read-only).
Verdict line: SHIP-A-AS-IS (no code change) with a required note amendment and a new follow-up issue "Staged winners audit defers on budget once the impact-fact corpus passes 1,000,000 rows, so the missed-mark backstop never runs"; R2 is not held.

Verified at origin/main 3d9ab0b5cf and A head abd0d83c34. Paths under go/internal; pg = storage/postgres. Skills loaded: postgres-rigor, performance-rigor. The arbiter ran no SQL; every QA number is the coordinator's.

## 1. Findings from source

1. The brief's reading is correct. `RunTick` walks every census pair, adds each batch to the tick totals, and aborts with reason `budget` past 20,000 rows in one pair, 1,000,000 staged facts, or 250,000 distinct keys (runtime/postgres/staged_audit_scan.go). The batch stages every eligible fact of the pair, not the pending keys (pg/supply_chain_impact_winners_staged_scan.go).
2. The scan must read the whole corpus by design. It recomputes a winner per key and diffs against the stored table (reducer/supplychain/staged/audit/tick.go). A missed mark is by definition not in the pending set, so a pending-only audit cannot find one.
3. The 1,000,000 cap is not a memory bound. The compactor folds each batch into per-key maps and never holds the staged rows (pg/supply_chain_impact_winners_staged_compact.go). Memory follows distinct keys. The fact cap bounds read work only.
4. The caps were sized for a much smaller corpus. The cap comment cites a worst scope of 10,742 facts (staged_audit_scan.go), and migration 291 records about 1.13M finding rows on QA (pg/migrations/291_...winners_keyed_idx.sql). Q1 now reads 8,274,197.
5. The check runs after the fetch. A tick that will defer still reads batches until the running total passes 1,000,000, then discards them. On QA that is roughly an eighth of the corpus read on the standby every 10 minutes for no result. The cost is unmeasured.
6. A deferred tick is not a success in A. It returns before `RecordSupplyChainImpactWinnersCommit` (reducer/supplychain/core/winners_keyed_maintainer.go), so `audit_last_success_age_seconds` keeps growing. The alert signal A adds is correct for this case.
7. "Very likely" is not yet proven. A's own note says QA keeps about 13 facts per identifier (docs/internal/evidence/11117-winners-contract.md). With about 80,000 winners that is about 1.04M eligible facts, which sits at the cap, not eight times over it. Q1 is an upper bound before three exclusions. Section 4 settles it.
8. A package_id range read has an index. `fact_records_supply_chain_impact_winners_keyed_idx` leads with `(payload->>'package_id')` under the finding-kind partial predicate (migration 291). The canonical key contains package_id, so a key never spans two package_id values. The winners table has a package_id column and no index that leads with it (migration 033).

## 2. Decisions

1. Q1, A's content. A merges with no code change. The addendum's two conditions are met: the verify is measured (9-14 s and 22.9-27.6 s against 30 s) and the audit has operator signals. The budget deferral is a defect of the staged reader on main, not of A.
2. What the audit gives QA after A. If section 4 confirms the cap is passed: nothing. QA will have no automatic backstop. Today's only working repair on QA is the accidental startup full on each roll, and A removes it. That trade stands: the startup full is a measured harm on every roll (3h33m-3h41m with no keyed commit), and the missed-mark harm has no observed instance. Convergence still rests on marks staged in the writer's transaction.
3. The backstop after A on QA is the force-full runbook step, and it is manual. Each use is a QA data mutation and needs its own arbiter review with readings (arbiter-11103c section 6). It runs one single-statement full of about 6,400 s. Use it on evidence, not on a schedule.
4. Required amendment to A before the PR opens (docs only). In 11117-winners-contract.md, replace the NOT_CHECKED sentence at lines 148-153 with the measured Q1 figures, the section 4 result, and the new issue number. State plainly that at `staged-audits` routing past the cap, QA has no automatic backstop until that issue ships. The PR body must say the same. A PR that calls the audit "the backstop" while the note shows it cannot run contradicts its own claim.
5. AC5 addition. After the NULL-version full commits, read `staged_audit_deferrals_total{reason="budget"}`, `_last_staged_facts`, and the deferral log's `read_seconds` for three consecutive ticks. If every tick defers on budget and `read_seconds` exceeds 60 s (the unrenewed lease TTL, so two pods read at once) or standby replay lag rises during the read, set `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL=0` on QA. Record it as a temporary state that ends when the new issue ships. If the first tick completes, the brief's premise is wrong; report the reading and close the new issue as a headroom issue at lower priority.
6. Q2, the design. A deterministic sweep by package_id range, across ticks, with a persisted cursor, read on the standby.
   - Each tick takes the next range of whole package_id values, bounded by a fact budget, from the keyed index. It recomputes winners for that range in one statement and one snapshot, diffs against the stored winners in the same range, and sends differing keys to the existing writer verify.
   - One snapshot per range removes today's cross-batch skew for a key's holders. The writer verify stays authoritative.
   - The cursor and the last completed sweep time persist on the writer, written under the audit lease. A process-local cursor repeats the #11117 defect: a roll would restart the sweep.
   - A range must cover stored winners no longer produced, facts with a NULL or empty package_id (a range predicate on the expression skips NULL), and one package_id larger than the budget (it runs alone and may not be truncated).
   - Operator overrides load whole on every range. They may lack package_id and they match across keys (pg/supply_chain_impact_winners_keyed_sql.go, `addWinnersKeyedOperatorFactsSQL`).
   - The corpus caps become per-range guards. No cap may skip a range forever.
   - This shares its partition primitive with #11119. One design review covers both; the first to land owns the primitive.
7. Options ruled out.
   - Raise the caps: every tick re-reads 8M facts through 11,619 sequential statements to find about zero repairs, and the cost grows with the corpus (7x since migration 291). The read time is unmeasured and may exceed the 10-minute interval. It is allowed only as a measured baseline (T0 below), not as the fix.
   - Verify only the pending candidates: cannot find a missed mark (finding 2). Accuracy.
   - Random sampling: no bound on when a given stale key is visited. Accuracy.
   - Hash-partition the keys per tick over the scope-ordered scan: each tick still reads the whole corpus and keeps a fraction. Hides the cost.
   - Switch QA to writer-backed routing: that is the single 6,400 s statement on the writer, the #11119 defect.
   - A statement timeout, a longer interval, or audit-off as the end state: guards and levers, not fixes.
8. Q3. This is a new issue. #11119 bounds full recomputes that write; #11120 covers the keyed apply phase; #11129 covers staging reads. None covers the staged reader's corpus budget. Text in section 5.
9. Q5. It does not gate R2. It does not gate R3 (#11103). The new issue is deferred until A merges, then design-first under arbiter review together with #11119.

## 3. Required proof for the new issue (theory first)

- T0 baseline: time the shipped per-pair batch over the full census on a disposable clone or the replica, read-only. This is what "raise the caps" would cost per tick.
- T1 one range: `EXPLAIN (ANALYZE, BUFFERS)` of the range recompute on representative data. Bar: index plan on the keyed index, no Seq Scan on `fact_records`, under 25 s cold, buffers proportional to the range.
- T2 stored side: the stored-winners read for one range. If it needs an index on winners package_id, measure the write cost on the keyed apply path.
- T3 differential: the union of all ranges equals the single-statement audit's candidate set on the same snapshot, with zero missing and zero extra keys. Derive the range SQL from the shipped winner body, not a hand copy.
- T4 skew: the largest package_id on QA by fact count, and the range count for a full sweep.
- Work proof on the built binary: a seeded stale winner, a seeded missing winner, and a seeded orphan stored winner are each repaired within one sweep; a roll mid-sweep resumes from the cursor; two pods never advance the cursor twice for one range; a keyed tick during a range verify is Skipped and commits next tick.

## 4. Q4: read-only QA measurement (replica)

Run M-A first. It is exact for the source arm and reads no payload, so it costs about what Q1 cost plus two primary-key joins. It keeps the shipped joins and predicates verbatim (pg/supply_chain_impact_canonical_winners_store.go). Both joined tables have `finding_fact_id` as primary key, so the joins add no rows.

```sql
SET statement_timeout = '900s';
SET default_transaction_read_only = on;
-- M-A: exact source-arm StagedFacts and the per-pair cap.
WITH census AS (
SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
WHERE generation.status = 'active'
),
per_pair AS (
SELECT fact.scope_id, count(*) AS rows_staged
FROM fact_records AS fact
JOIN census
  ON census.scope_id = fact.scope_id
 AND census.active_generation_id = fact.generation_id
LEFT JOIN scanner_image_result_fact_classifications AS finding_class
  ON finding_class.finding_fact_id = fact.fact_id
LEFT JOIN scanner_image_result_fact_associations AS finding_association
  ON finding_association.finding_fact_id = fact.fact_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE
  AND fact.scope_id <> 'operator:vulnerability_suppressions'
  AND finding_association.finding_fact_id IS NULL
  AND fact.scope_id NOT LIKE 'image://derived/%'
  AND (finding_class.finding_fact_id IS NULL
       OR (finding_class.source_class = 'static' AND finding_class.lifecycle = 'active'))
GROUP BY fact.scope_id
)
SELECT count(*) AS pairs_with_rows,
       sum(rows_staged) AS staged_source_facts,
       max(rows_staged) AS max_rows_per_pair,
       count(*) FILTER (WHERE rows_staged > 20000) AS pairs_over_batch_cap
FROM per_pair;
-- M-B: distinct-key proxy (exact for stored winners; cap is 250,000).
SELECT count(*) AS stored_winners FROM supply_chain_impact_canonical_winners;
```

Reading: `staged_source_facts > 1,000,000` or `pairs_over_batch_cap > 0` proves every tick defers. The operator arm adds at most one scope's rows and cannot lower the total. If M-A times out, take the exclusion bound: Q1 minus `count(*)` of the associations table minus `count(*)` of classifications where not (static and active) minus the Q1-shape count restricted to census scopes `LIKE 'image://derived/%'`. A result above 1,000,000 is still proof; a result below is inconclusive.

Optional M-C for T0: `EXPLAIN (ANALYZE, BUFFERS)` of the shipped source batch for the three largest pairs from M-A. Seconds per staged fact times `staged_source_facts` estimates the full read.

## 5. New issue

Title: Staged winners audit defers on budget once the impact-fact corpus passes 1,000,000 rows, so the missed-mark backstop never runs
Labels: bug, backend, capability:reducer-scale, reliability, priority:high
Refs: #11117, #11119, #10451. Epic: the same one as #11119 if it has one (NOT_CHECKED).
Acceptance criteria:
- [ ] M-A and M-B recorded with the QA figures; T0-T4 recorded with plans before any production code.
- [ ] No staged-audit tick reads work proportional to the whole corpus; a regression test seeds a corpus past today's caps and the audit completes a sweep.
- [ ] The sweep is deterministic and complete: every package_id range, the NULL and empty package_id rows, and stored winners with no source are visited once per sweep; T3 shows candidate-set equality.
- [ ] The cursor and last sweep completion persist and survive a roll; two pods do not double-advance; live-Postgres contention proof.
- [ ] Each standby statement stays under 25 s and each writer verify under 30 s at QA shape; a full sweep completes within 24 h on the QA corpus, with the measured period recorded.
- [ ] Seeded stale, missing, and orphan winners are repaired within one sweep on the built binary.
- [ ] Telemetry: sweep position, last sweep completion age, ranges per sweep, and deferrals by reason; the telemetry reference is updated; `audit_last_success_age_seconds` keeps meaning a completed pass.
- [ ] Live QA reading after deploy: one completed sweep, its duration, candidates, repairs, and discards.

If M-A shows the corpus under the cap, keep the issue, retitle it to the lost headroom, and drop priority:high.

## 6. Will not do

Hold A or R2 for this. Keep the startup full as a safety net. Raise the caps in A. Narrow the audit to pending keys. Sample. Route QA's audit to the writer. Schedule periodic forced fulls. Treat a timeout or `AUDIT_INTERVAL=0` as the fix. Fold this into #11119, #11120, or #11129. Each one either trades accuracy, hides the cost, or re-creates the #11117 defect.

## 7. Unverified

- Every QA number. Q1 is an upper bound; whether eligible facts exceed the cap rests on M-A. The 13-facts-per-identifier figure makes a borderline result possible.
- The standby read cost of a deferred tick and of a full read (T0), and whether deferred ticks ran on QA before A (no metric existed).
- That the planner takes the keyed index for a package_id range, and the cost of the stored-side range read (T1, T2).
- Skew of facts per package_id on QA (T4), and the 24 h sweep bar, which is a ceiling set without a measurement.
- Whether any capture hole exists on QA today. No audit has ever completed there, so there is no evidence either way.
- The #11119 epic link and the lease takeover semantics at expiry (carried from the addendum).
- M-A was not executed by the arbiter; it is derived from the shipped predicates and the coordinator's Q2.

## Coordinator note

The new issue in section 5 was filed as #11131. Per decision 9 it is deferred until A merges, then design-first under arbiter review together with #11119.

## Reading recorded after the ruling

The exact M-A grouped count timed out at 900 s on the QA replica
(2026-11-21 11:01Z). The coordinator used the exclusion lower bound instead,
which is conclusive when it is above the cap: 8,274,197 active finding facts in
census pairs, minus 15,987 classification rows that are not static and active,
0 scanner-association rows and 0 derived-image scopes, gives at least
8,258,210 staged source facts, more than eight times the 1,000,000 cap. Stored
winners (M-B) are 79,614, under the 250,000 distinct-key cap. Every staged
audit tick on QA defers on budget, so #11131 keeps priority:high.
