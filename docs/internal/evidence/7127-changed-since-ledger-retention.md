# #7127 PR-3d: changed-since ledger retention

Generation retention now prunes the changed-since link ledger that PR-3a
(migration 136) writes. This follows the design ruling's 2.8 rule, as settled
by arbiter ruling arb-7127-3d: ship the literal rule (A) as one statement.
Option B (a sweep of rows that name gone generations) is rejected. C (the
writer fencing the prior generation) is PR-3e. Arbiter ruling arb-7127-3d-b
fixed the row-limit starvation the ledger counts introduced (a generation over
the limit only by its ledger rows is pruned in a batch of its own), and
arb-7127-3d-c narrowed such a batch's locks to its own scope and generation
row after PB4's lock-hold gate failed.

Sections followed:

- **2.8.** In the prune transaction, delete the links, link deltas and bucket
  counts whose `generation_id` or `prior_generation_id` is a pruned
  generation, and the activation rows of the pruned generations. The state
  table is untouched. The ledger rows count toward `BatchRowLimit`.
- **8.4.** Take the pairs from `changed_since_links`, delete by key prefix, and
  add no index. The plan-shape sweep covers the statements.
- **8.10, PR-3d note.** The ledger SQL lives in `linksfreshnessstore`, and the
  root passes the candidates' scope and generation ids as parallel arrays.
- **arb-7127-3d.**
  - The delete is one statement.
  - `RowsPruned` comes from its returned counts, and the pre-count only feeds
    the limit and the events, with a WARN when the two differ.
  - The bound is written down.
  - Activation rows are never deleted on `prior_generation_id`.
  - The delete runs with the link switch off.

No environment variable is added.

## What runs

`PruneSupersededGenerations` (`generation_retention.go`) locks candidates
`FOR UPDATE OF generation, scope SKIP LOCKED`. Then:

1. `countRows` runs the existing row count and
   `linksfreshnessstore.PrunedGenerationRowCounts`, merged per generation, so
   ledger rows count toward `BatchRowLimit` and appear in the events.
   - A link naming two candidates is charged to the newer.
   - Deltas are counted from `changed_since_links.delta_rows`.
2. Selection (`selectCandidatesWithinRowLimit`, oldest first): a candidate
   whose rows outside the ledger exceed the limit is skipped (`row_limit`,
   ADR #2248); the first candidate is admitted whatever its total; a later
   candidate that would push the batch over the limit is deferred
   (`row_limit_ledger` if it alone is over the limit, else `row_limit`). So a
   batch of two or more stays within `BatchRowLimit`, and a batch of one may
   exceed it by its own ledger rows only.
3. After a skip the batch is recounted and **the limit is checked again**, at
   most `generationRetentionRecheckLimit` (3) times; after that the batch keeps
   its first member and recounts it once. A link shared with a skipped
   generation is still deleted with the selected one, so its charge moves
   there and a recount can grow; the store used to assume a recount only
   shrinks.
4. The selection runs inside `SAVEPOINT retention_selection` (after
   `SET LOCAL work_mem`). A batch of one over the limit rolls back to it, which
   releases every scope and generation row the selection locked, re-locks only
   its own scope and generation (`generationRetentionTargetedCandidateQuery`,
   the candidate query's predicates for one pair, `SKIP LOCKED`), and recounts
   under that lock. No row from the re-lock: the pass prunes nothing and writes
   no event. Any other batch releases the savepoint and runs unchanged.
5. Before `scope_generations` is deleted,
   `linksfreshnessstore.DeletePrunedGenerationRows` runs `retentionPruneQuery`.
   It is one `WITH` that deletes the doomed links' deltas and bucket counts
   (LATERAL `OFFSET 0` probes of the full key prefix, then a ctid delete), the
   links themselves, and the activation rows of the candidates. Its returned
   counts are `RowsPruned` and `LedgerRowsPruned`. The pre-count is
   `LedgerRowsCounted`, and the runner logs a WARN with both when they differ.
6. `RowsOverLimit` (log `rows_over_batch_row_limit`, counter
   `eshu_dp_generation_retention_over_limit_batches_total`) records a batch of
   one over the limit, and `LockedScopeRows` (log `locked_scope_rows`) the
   scope rows it held at the delete.

## Why `retention_expired` is not the protection

Nothing reads the ledger yet; the read path is PR-3c. The changed-since read
resolves the since generation from `scope_generations` first and consults the
retention events only when that finds nothing (`changed_since.go`). So the
protection against an orphan link is this: **a pruned generation can never be
the since generation**. Beyond that:

- An orphan `(X -> G)` is a true row. The state was the effective state at X.
- It is unreachable from a live read, because every link whose `generation_id`
  is X was deleted with X.

## The bound (arb-7127-3d, rationale 2)

Per scope, at any time:

- **At most one link whose prior is gone.** Its deltas are at most the keys of
  X and G together, and its bucket counts at most 18 rows. It is removed when
  its `generation_id` is pruned.
  - Exactly one incremental link in a scope's history has prior X.
  - The next link has prior G. It can be an orphan only if G was pruned first,
    and the batch that pruned G deleted `(X -> G)` by the generation side.
  - This holds for the incremental chain only. A `pairwise` writer that does
    not lock both ends breaks it.
- **Activation rows from a backfill that raced a prune:** at most one chain
  (64 rows) per backfill episode. They are removed only when the scope is
  purged.

The 7.5 budget line ("`changed_since_link_deltas` bounded by generation
retention") is amended to: bounded by generation retention **apart from the
rows of this bound**.

An activation ends in one of three ways: as one link, as one break, or deleted
by retention before the writer reaches it. The third ends without a link or a
break (P4).

## BatchRowLimit and ledger-heavy generations (arb-7127-3d-b, arb-7127-3d-c)

Counting ledger rows toward `BatchRowLimit` per candidate made a new class of
generation unprunable (ruling B, rationale 2):

| Case | At `origin/main` | With ledger counts, before ruling B | Now |
| --- | --- | --- | --- |
| Rows outside the ledger > limit | skipped for good (ADR #2248) | same | same (filed separately) |
| Outside <= limit < outside + ledger | pruned | **skipped for good**, and so was the other generation of an oversized link | pruned in a batch of its own |
| Total <= limit | pruned | pruned | pruned |

The RED record is `TestRetentionRowLimitDoesNotStarveLedgerHeavyGenerations`
at 4a3e229582: after three passes at limit 60, c1 and c2 remained, both
skipped as `row_limit_ledger` every pass. Now all three go in three passes and
the batch that deletes the oversized link holds one generation.

`row_limit_ledger` means "deferred to a batch of its own": such a candidate
leads a later batch. It is never stuck unless its rows outside the ledger
exceed the limit.

### PB4: the lock-hold gate failed at 1,542,402 rows

PB4's gate, declared before the run: the prune transaction deleting the
1,542,402-row link lasts at most 15 s. The rig ran Postgres at 128MB
`shared_buffers`. The fixture had 25 scopes and 100 candidates. There were 6
interleaved rounds, with a run counted valid when the load at its start was
under 18. **Two of six valid runs were 17.6 s and 20.2 s: the gate failed**
(median 8.4 s; 5.4 µs per deleted row at the median, 13.1 µs at the worst). At
771,201 rows all 5 valid runs were under 15 s (median 2.26 s, worst 2.82 s).
The executor stopped.

Ruling C's diagnosis (its experiment 1, on these templates; the arbiter's
buffer counts, not this PR's timings):

| Link | shared hit | shared read | temp | Touches per deleted row |
| --- | --- | --- | --- | --- |
| 771,201 | 2,809,683 | 318,684 | none | 4.06 |
| 1,542,402 | 5,123,653 | 1,133,070 | none | 4.06 |

- The statement's work is linear: 4.06 buffer touches per row at both sizes.
- The theory that a CTE spilled past `work_mem` is **disproven**: there is no
  temp I/O in either plan.
- What grew 3.6x for 2x the rows is buffer *reads*. The delta table and its
  key are 716 MB at 1.54M, against a 128MB cache.

A 20 s hold on up to 100 locked scope rows is avoidable: the wide lock set
exists for the selection, not for the delete. So ruling C narrowed it (step 4
above).

### PC1: the narrowed lock set

- **(a)** `TestGenerationRetentionNarrowsTheLockSetForAnOverLimitBatch` (fake
  database).
  - A batch over the limit runs: work_mem, SAVEPOINT, candidate query,
    counts, ROLLBACK TO SAVEPOINT, targeted lock, one count pair, then the
    same prune sequence as a normal batch.
  - A normal batch runs: SAVEPOINT, the base selection, RELEASE SAVEPOINT,
    then the prune.
- **(b)** `TestRetentionTimingP8` in `lockprobe` mode, 128MB rig, 1,542,402-row
  link. While the delete was running (`pg_stat_activity`), `FOR UPDATE NOWAIT`
  found only `tscope-00` and `tscope-00-g0` held, out of 26 scope rows and 625
  generation rows. The delete was still running after the probes.
  - RED: a binary with the `ROLLBACK TO SAVEPOINT` removed held all 25 scopes
    and every candidate generation, and NOWAIT on `tscope-01` failed with
    55P03.
- **(c)** `TestGenerationRetentionNarrowedCandidateTakenElsewhereLive`. A
  second session takes the candidate between the rollback and the re-lock.
  The pass commits, prunes nothing, writes no event and returns no error. The
  next pass prunes it alone with `locked_scope_rows` 1.

### PC2: the lock hold of a batch of one

The PC2 runs moved to
[7127-ledger-retention-lock-hold.md](7127-ledger-retention-lock-hold.md): PC2
under ruling C's load rule (failed), PC2-D under rule PD (gate (ii) failed as
written; ruling E withdrew it), PC2-E (the gated result at `494d1439d4`,
before the rebase onto #7329), and PC2-G, the gated result on the shipped
code (arbiter ruling arb-7127-3d-g).

### PB7: the P8 fixture drained

At the default limit (100,000), on the P8 `link` fixture:

| Template | Base (4a3e229582) | After the fix |
| --- | --- | --- |
| Fresh, 40 candidates | 2 passes; the 250k-link generation starves | 3 passes; pass 2 prunes that generation alone (656 ms); nothing left |
| Aged, 402 candidates | 7 passes; 2 generations starve | 8 passes; pass 2 prunes it alone (1,056 ms); nothing left |

The "1 of 40 skipped" figure of P8 is now: 0 skipped for good. The generation
is deferred once, then pruned in a batch of one.

## Proof (arb-7127-3d P1-P10)

All live tests run on PostgreSQL 18.6 (`postgres:18-alpine`), in databases
cloned from the full bootstrap.

| Id | Test | Result |
| --- | --- | --- |
| P1 | `TestRetentionRuleP1` | Scope linked g1..g5, prune g1 and g2. The remaining rows of all six tables hash-equal the expected set. State and cursor rows are byte-identical. A second pass changes nothing. RED: the planted generation-side-only statement leaves `(g2 -> g3)` |
| P2 (a) | `TestGenerationRetentionDeletesTheLedgerInOneStatement` (fake DB) | Exactly one statement of a prune deletes from the ledger, and it deletes from all four tables |
| P2 (b) | `TestRetentionDoesNotWaitOnALinkInFlight` | An uncommitted `(x1 -> x0)` link; the prune of x0 returns without waiting (84-170 ms). The link commits whole, with 0 headless deltas and 0 headless bucket groups |
| P2 RED | `TestSplitLedgerDeleteLeaksHeadlessDeltas` | The same deletes as separate statements, with the real writer committing between them, leaked 8 delta rows with no link row |
| P3 | `TestRetentionBoundP3` | Prune X, then the real writer links `(X -> G)`. The probe reads 1/0/0. `ComputeChangedSinceDelta` from X answers `retention_expired`. After G's prune the probe reads 0/0/0 and no row names X or G |
| P4 | `TestRetentionActivationAboveCursorP4` | G1's activation is deleted above the cursor, and the backlog drops by one with no link or break. G2 then links incremental from X, with the state equal to G2's aggregate |
| P5 | `TestRetentionCountsP5`, `TestRetentionRowLimitP5` | Event sums equal the deleted rows. A two-candidate link is charged once, to the newer. The pre-count (`SUM(delta_rows)`) equals the delete. After a skip, the recount is re-checked: a limit equal to the recount prunes within it; one below prunes the generation alone, one row over. RED: a planted count charging both candidates breaks the sums |
| P6 | `TestRetentionStatementPlanShape` | Details below this table |
| P7 / PB5 / PC3 | `TestLinkAndRetentionProcessesRace` (reducer package, three OS processes of the test binary) | Details below this table |
| P8 | `7127-ledger-retention-timing.sh` + `TestRetentionTimingP8` | Below |
| P9 | `TestGenerationRetentionLogCarriesLedgerCounts`, `TestGenerationRetentionWarnsWhenLedgerDeleteDiffersFromCount` | The cycle log carries `changed_since_ledger_rows_pruned`. A WARN carries both figures when they differ. The rows-pruned counter has the four table labels, registered in the coverage doc |
| P10 | docs | The bound and the third ending are in the link store README and AGENTS.md and here. The 7.5 line is amended (above). The maintenance README names the ledger tables. G14 is narrowed to "the link domain issues no SQL" |

**P6 details.** The fixture is 2,000,000 delta rows over 500 scopes and 5,000
links, with a 100-candidate batch. Both statements go through the driver's own
`[]string` binding, in auto, custom and generic modes, under four statistics
states: never analyzed, fresh, analyzed empty then loaded, and stale. In every
case:
- deltas and bucket counts are read by their key on all three prefix columns,
  at most 10 buffers per row, or by a Tid Scan fed from such a read;
- no Seq Scan touches the deltas;
- links and activations are each read once;
- no join has a CTE, Seq, Function or Materialize scan as its inner child.

REDs: the planted per-scope LATERAL fails (`Seq Scan on changed_since_links
ran 100 times`), and so does the delta table without its key.

**P7 details.** The link runner and two retention processes run at
`BatchRowLimit` 2,100, at most four generations per batch, over 24 scopes with
g0 and g1 prunable while being linked. Every third scope rewrites all 2,000
keys per generation, so its links are over the limit. The latest run:
- 57 links, 43 `generation_locked` retries, 0 `pruned_before_link` breaks.
- The two retention processes pruned 25 and 23 generations: all 48, none
  starved. 32 batches were over the limit.
- **(a)** No link names a pruned generation on its generation side.
- **(b)** The longest retention batch took 186 ms.
- **(c)** The 43 `generation_locked` retries.
- **(d)** No headless delta or bucket row. At most 1 orphan link per scope
  (0 in this run).
- Exactly one retention event per generation: no generation was pruned twice.

**Statements (PB5/PC3).** A scratch database double recorded every statement
of three prunes (one candidate within the limit, three candidates, a row-limit
skip with recount) at 4a3e229582 and after the fix. With the `SAVEPOINT` /
`RELEASE SAVEPOINT` pair removed, the 49 statements are identical in hash and
order. Only a batch of one over the limit differs, as PC1 (a) shows.

In the arbiter's P6 fallback, `del_ctid.sql`, the links are read through a
per-candidate `LATERAL`. That is the shape the stale-statistics RED of P6
fails, so the shipped links read keeps array filters. The deltas and bucket
counts use the fallback's LATERAL plus ctid form.

## P8: lock hold with a 250,000-row link

The gate was declared before the run:
- ledger empty: after/before paired median at most 1.10;
- with the link: added hold at most 15 s.

The lock hold is the prune transaction's duration. The candidate lock is its
first real statement, and commit releases it.

Before = base `bd2d98a8d4`; after = this change. Both binaries were built from
the same timing test. There were 6 interleaved rounds per fixture, with the
first mover alternating. Each run clones a template, so storage state is
identical. The fixture is 20 scopes of 26 generations with 400 content facts
each (208,000 fact rows); the batch is 40 candidates.

The 1-minute load was 16.6-17.8 on 18 CPUs, so **all 24 runs are valid** under
rule 8.2. That margin is thin.

| Fixture | Before, ms (6 runs) | After, ms (6 runs) | Median before / after | Paired |
| --- | --- | --- | --- | --- |
| Ledger empty | 450, 293, 277, 334, 331, 316 | 327, 301, 349, 365, 373, 309 | 323.5 / 338.0 | ratio 1.06 (gate 1.10: pass) |
| 250,000-row link among candidates | 312, 297, 365, 334, 389, 288 | 985, 913, 2,126, 977, 948, 830 | 323.0 / 962.5 | +629.5 ms (gate 15 s: pass) |

With the link, "after" deletes 253,900 delta rows, 40 links, 40 bucket rows
and 40 activations; "before" leaves all 299,900 delta rows. So the scope and
candidate generation rows are held about 0.96 s (median, 2.1 s at the worst)
instead of about 0.32 s.

## Observability

- `eshu_dp_generation_retention_rows_pruned_total{table}` carries the four
  ledger tables.
- `eshu_dp_generation_retention_skipped_total{reason}` adds
  `row_limit_ledger`.
- The retention cycle log carries `changed_since_ledger_rows_pruned`, and a
  WARN fires when the delete differs from the pre-count.
- New gauges `eshu_dp_changed_since_deltas_bytes` and
  `eshu_dp_changed_since_deltas_rows` report the link-delta table's size.
- `eshu_dp_generation_retention_over_limit_batches_total` counts batches of
  one admitted over the limit. The cycle log carries
  `rows_over_batch_row_limit` and `locked_scope_rows`.
- No span: the retention runner has none.

Performance Evidence: P8 above: ledger empty, paired ratio 1.06; with a
250,000-row link, +629.5 ms of lock hold, over 24 valid interleaved runs.
PC2-G (lock-hold page, run on `6c5cfec8cb`): a batch of one deleting a
1,542,402-row link holds one scope row and one generation row (PC1 (b)) for
2.90 s median, 3.53 s worst, at `shared_buffers` 2GB (4.76 s and 5.78 s at
128MB). P6
covers plan shape at 2M delta rows in four statistics states and three
plan-cache modes.

Observability Evidence: the `table` labels for the four ledger tables, the
`row_limit_ledger` skip reason, the `changed_since_ledger_rows_pruned`,
`rows_over_batch_row_limit` and `locked_scope_rows` log fields and the WARN
(`TestGenerationRetentionLogCarriesLedgerCounts`), the over-limit batch counter
(`TestGenerationRetentionOverLimitBatchTelemetry`), and the delta-table gauges
(`TestRecordGaugesReportsLinkDeltaTableSize`).

## Shim (prove the theory first)

`7127-ledger-retention-shim.sql` builds 2,876,801 delta rows, including a
771,201-row full-rewrite link, with statistics analyzed at 5 scopes.

| Links lookup | Stale statistics | Verdict |
| --- | --- | --- |
| Join to the candidates | One seq scan of the links; the join order is a planner choice | Not adopted |
| Per-scope LATERAL | `Seq Scan on changed_since_links`, loops=100 | Rejected (the G8 class) |
| Array filters, no join | One scan of the links | Adopted |

Other results:
- Summing `delta_rows` cost 34-41 buffers; `COUNT(*)` over the deltas read
  802,319 buffers.
- The final statements had 0 seq scans in 16 plans.
- A 775,601-row delete touched about 4 buffers per row. Its timings in that
  shim were taken at load 27-88 and are invalid.

## Not measured

- The same statements on ops-qa. The writer stays off there until this
  merges; the orphan probe is to be run by hand during the dark window (ruling
  8.7 addition).
- The orphan rate. Its source, the prior-fence fix, is PR-3e.
- A link past about 1.65 million rows, where the delete's list of row ids
  outgrows the transaction's 64MB `work_mem` and spills to temporary files.
- The cause of the lock hold's wall-time excess over linear (about 2.3-2.4x
  for 2x rows with exactly linear buffer work); see the WAL observation in
  [7127-ledger-retention-lock-hold.md](7127-ledger-retention-lock-hold.md).
