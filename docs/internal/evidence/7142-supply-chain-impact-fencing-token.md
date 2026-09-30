# Supply-Chain Impact Fencing Token (#7142)

## Defect

#6831 made each supply-chain impact pass the complete truth for its
`(scope, generation)`: under a per-pair advisory lock it upserts its findings
and tombstones every other active row. The writer stamped every row with
`fencing_token = 0` and the retraction passed `0`, so `fencing_token <= 0`
matched every row the domain wrote and the last committer won the whole set.

The heartbeat mitigation (a failed heartbeat cancels the pass context) does not
cover a worker that is frozen rather than failed: it resumes after its item was
reclaimed and another worker already committed fresher evidence. That worker
then writes an older evidence set, and with the #6831 retraction it tombstones
the fresher pass's rows. Before #6831 it could only add rows.

Root-Cause Evidence: a live interleave with the real handler, writer and
Postgres. Pass A issues its token, reads evidence, and stalls; pass B (fresher
evidence, fresher token) runs to completion and commits `[r_b]`; A resumes and
finishes last. With the model of main (no admission check, rows left at token 0,
retraction bound to 0) the active set after A finishes is `[r_a]`: B's fresher
finding was retracted and A's stale one published. With only the admission
disabled but rows stamped, the set is the union `[r_a r_b]`, truth neither pass
derived.

## Fix

Design ruled by the arbiter (token source, admission, migration shape).

- **Token**: a Postgres sequence, `supply_chain_impact_fencing_token_seq`,
  issued by `SupplyChainImpactHandler.Handle` after its nil checks and before
  the evidence load (`SupplyChainImpactFencingTokenIssuer`, implemented by
  `postgres.PostgresSupplyChainImpactFencingTokenIssuer`). Every `Handle` call,
  including a retry or a redelivery after reclaim, draws a fresh token. The token
  orders passes by when they began reading evidence: a pass that read its
  evidence and then stalled can never publish over a pass that began later. It
  does not order passes whose loads interleave. The load is many statements with
  no snapshot, so a pass that took its token first but read later can hold the
  lower token with the fresher evidence, and a pass that began after it can then
  commit later and retract that finding. That residual is no worse than before
  the fence (the last committer won) and converges only when a later intent for
  the same `(scope, generation)` runs; the stronger property needs a snapshot
  across the load and is a separate design. A commit-time token was rejected: it
  orders by commit, so a worker that read stale evidence early but committed last
  would out-rank the fresher one that committed first. A pass the readiness floor
  defers burns a value, a harmless sequence gap. A work-item-derived token was
  rejected: `fact_work_items` has `attempt_count` but no claim generation, and a
  re-drive or second intent for one `(scope, generation)` creates a different
  item, so it is not comparable across items. The reducer host clock was rejected
  for the reason migration 089 records for `aws_cloud_runtime_drift` (clock skew
  inverts the order).
- **Admission**: a watermark table, `supply_chain_impact_write_admission`, one
  row per `(scope_id, generation_id)` holding the highest token any pass was
  admitted with. The writer runs a compare-and-set upsert inside the write
  transaction, right after the advisory lock: it applies only when
  `stored.fencing_token <= incoming`, and `RowsAffected() == 0` rejects the pass
  whole, before any upsert or retraction. `<=`, not `<`, so re-executing the identical write value
  (same token) is admitted; a queue retry or redelivery is a new `Handle` call with a
  fresh token. Every admitted pass writes the row, including
  an empty or partial one, which is why a `MAX(fencing_token)` scan over
  `fact_records` was rejected: a fresher pass that derives an empty set leaves no
  row to take a maximum over and the stale pass would be admitted.
- **Rejection**: `supplyChainImpactWriteSupersededError`, retryable, failure
  class `supply_chain_impact_write_superseded`, enrolled in
  `nonCountingReducerRetryFailureClasses`. It is an error, not a success with
  zero writes: a zombie worker's `Fail()` is lease-fenced, so it cannot disturb a
  reclaimed item, a distinct older intent retries with a fresher token and
  converges, and dead-lettering would freeze stale truth. Non-counting because
  losing this race is not the item's fault.
- **Row stamping**: every upserted row carries the pass token, and the
  retraction becomes `SET is_tombstone = TRUE, fencing_token = $5 ... AND
  fencing_token <= $5`. A tombstone records the pass that retired it, and the
  existing upsert guard (`existing <= incoming`) then refuses a stale revive of
  it even without consulting the table: defense in depth.
- **Fail closed**: `SupplyChainImpactWrite.FencingToken` zero is rejected by the
  writer; `Handle` errors without an issuer; the domain is not registered unless
  the registry was given both the writer and the issuer. There is no legacy-0
  fallback, because a defaulted token would make the guards inert again.
- A `PartialEvidence` pass (#7154) runs the admission and stamps its token like
  any other; only its retraction is skipped, so a stale partial pass cannot
  upsert stale rows into a fresher set.

## Migration

`154_supply_chain_impact_write_admission.sql` creates the table, the sequence,
and a guarded seed that advances the sequence to `MAX(fencing_token) + 1` of the
admission table when that exceeds the sequence's `last_value`. The file
re-applies on every reducer start, so it can only move the sequence forward. It
seeds from the admission table only: every pre-existing finding row carries `0`,
every later non-zero token comes from this sequence, and an admitted watermark
is at least every row token of its pair, so no scan of `fact_records` and no
`fact_records` index is needed (unlike `aws_cloud_runtime_drift`, migration
089/090, whose old tokens were wall-clock values).

Rolling deploy: an old reducer binds `0`. Its upsert guard `T <= 0` rejects every
row a new pass stamped with a token, and its retraction `fencing_token <= 0`
skips them, so it cannot retract or overwrite new truth. It can only add rows at
`0` or revive `0` tombstones, which the next new pass retracts (`0 <= T`).

Rollback: rolling forward is safe, but a full rollback to a reducer that predates
this change freezes the impact findings the new reducer touched. The old reducer
binds `0`, so for every row and tombstone the new reducer stamped with a token
its upserts are refused silently (`existing <= 0` is false, no error, the pass
reports success), it cannot revive a finding the new reducer retired, and it
cannot retire a row. Before starting old reducers, reset the tokens:
`UPDATE fact_records SET fencing_token = 0 WHERE fact_kind =
'reducer_supply_chain_impact_finding' AND fencing_token > 0`. On a large install, run it in batches by `scope_id`: it rewrites every such
tuple, so one unbatched statement writes a lot of WAL and holds those row locks
until it commits. The admission table
and the sequence can stay: old code ignores them, and a later roll-forward keeps
issuing higher tokens than any watermark. `container_image_identity` and
`aws_cloud_runtime_drift` have the same property and document no rollback either.
The admission table gains one small row per `(scope, generation)` and is not
pruned by generation retention; `aws_cloud_runtime_drift` has the same property. The migration's re-apply seed does one `MAX(fencing_token)` over the table on every reducer
start; measured on local Postgres 18 at 1,000,000 rows (121 MB), it runs in 21-24 ms
(a parallel sequential scan of the whole table, about 15,000 pages; the cost grows
linearly with the table, about 53 ms with parallelism off), so the startup cost stays
small at any realistic age.

## Lock order and transaction scope

One transaction per pass, unchanged in shape: (1) `pg_advisory_xact_lock` on
`(fact_kind, scope, generation)`; (2) the admission compare-and-set; (3) the
chunked batched upsert; (4) the retraction (skipped for a partial pass); commit
or rollback together. The token is issued before the transaction by `nextval()`
on a plain connection, which is non-transactional and never waits on a lock.

No cycle is possible: the advisory lock on a key is taken before any row lock;
the admission row and every `fact_records` row of that key are reachable only by
holders of the key (a finding's fact id embeds its scope and generation), and
holders of one key are serialized by the advisory lock, so the admission row
lock never waits. Passes for different pairs share no rows and proceed
concurrently. A rejected pass rolls back before any row write.

## Edge cases

| Case | Outcome |
| --- | --- |
| Older token commits after the fresher set | rejected whole, retryable, non-counting |
| Older token, the fresher pass wrote an empty set | rejected (the watermark row exists) |
| Identical write executed twice (same write value, same token) | admitted (`<=`); a retry or a redelivery is a new `Handle` call with a fresh token |
| Stale partial-evidence pass | rejected; a fresher partial pass is admitted and raises the watermark |
| Legacy rows at token 0 | updated or retracted by any pass (`0 <= T`); the tombstone carries `T` |
| Old reducer during a rolling deploy | cannot retract or overwrite fenced rows |
| Zombie whose item was reclaimed and acked | rejected; its `Fail()` updates 0 rows (lease fence) |
| Two distinct intents for one pair | one sequence, comparable tokens; the loser retries with a fresh token |
| Loads interleave (A takes its token, stalls before reading; B begins later, reads older evidence, commits after A) | ordered by issuance, not by what each read: B can retract A's fresher finding, as before the fence; converges on the next intent for the pair |
| Different pairs | fully concurrent (distinct advisory keys, disjoint rows) |
| Sequence unavailable | `Handle` errors before the load |

Performance Evidence: local Postgres 18 (`postgres:18-alpine`, throwaway
container), one `(scope, generation)` holding 20,000 finding rows with 10,000
retracted (`EXPLAIN (ANALYZE, BUFFERS)` inside a rolled-back transaction).

- Retraction before, without the token stamp: 139.1 ms. After, stamping the
  token: 147.6 ms (+6%: one more column written on the tombstoned rows; the scan
  is the same). Buffers 131,656 versus 137,251, 253 to 268 pages dirtied either
  way.
- Admission compare-and-set on the primary key: 0.113 ms on the insert path, 0.058
  ms on the update path, 0.038 ms when it rejects; 3 to 21 buffers.
- `nextval()`: 0.168 ms per call over 2,000 sequential calls.

The one added statement per pass is a primary-key point upsert, and the one added
write per tombstoned row is a column the tuple rewrite already carries.

Test Evidence:

- Hermetic (`supplychain/core`): `TestSupplyChainImpactWriterStampsFencingTokenOnRows`,
  `...RetractionCarriesFencingToken`, `...RejectsSupersededPassBeforeUpsert`
  (0 rows from the admission: no insert, no retraction, one rollback, retryable
  class), `...RejectsZeroFencingToken`, `...PartialEvidenceStillAdmits`,
  `TestSupplyChainImpactHandleIssuesFencingTokenBeforeEvidenceLoad`,
  `...RequiresFencingTokenIssuer`, `...FailsClosedWhenTokenIssueFails`,
  `...ReportsSupersededWrite` (counter and WARN fields); the statement order test
  now pins lock, admission, upsert, retraction. Registration tests in the
  reducer root pin the issuer gate. Queue tests pin the non-counting class on
  both the fail path and the claim path.
- Live Postgres (ledger class `scheduled`): the goal proof
  `TestSupplyChainImpactStalePassResumingAfterReclaimCannotRetractFresherSetLive`
  (the stale pass finishes last and is rejected; the active set is exactly B's;
  no A row exists; the watermark stays at B's token; the counter is 1),
  `...EmptyFresherPassStillFencesOlderPassLive` (fails against a `MAX` fence),
  `...RetractsLegacyZeroRowsAndStampsTokenLive`,
  `...UpsertGuardRefusesAStaleReviveLive`, and
  `...FencingTokenIssuerIssuesStrictlyIncreasingValuesLive` (200 concurrent
  values distinct and increasing per caller, a re-applied migration never
  regresses the sequence below an admitted watermark). The existing #6831 live
  proofs were adapted: passes carry increasing tokens, and the concurrency proof
  now asserts the survivor is the freshest pass's set (before: whichever pass
  committed last).
- Mutation: with the admission rejection disabled the goal proof and the empty
  fresher-pass proof fail (`stale pass A returned <nil>`).

Observability Evidence: a pass rejected at admission increments
`eshu_dp_supply_chain_impact_write_superseded_total` (label `domain`) and logs
one WARN line, "supply chain impact write superseded", with `scope_id`,
`generation_id`, `intent_id` and `fencing_token`; the queue also counts the
retry in `eshu_dp_reducer_retry_surge_total{failure_class}`. A steady rate under
continuous ingest is normal churn; a scope that stays superseded points at two
workers repeatedly overtaking each other.

No-Regression Evidence: the retraction predicate and keep set are unchanged; the
lock order gains one primary-key statement (measured above). The #6831 live
proofs pass with the token semantics.
