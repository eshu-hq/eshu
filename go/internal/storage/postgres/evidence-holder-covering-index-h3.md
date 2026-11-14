# H-3 covering-index decision record (#10564 pricing, #10643 mechanism)

**Verdict: no-build.** The covering pair-arm indexes
`fact_records_sci_cve_scope_gen_idx` (`((payload->>'cve_id'), scope_id,
generation_id)`) and the advisory twin
`fact_records_sci_adv_scope_gen_idx` stay unbuilt. The #10564 gate priced
them as no-build on economics; the #10643 lane re-fired the gate on real
guard pressure and then disproved the mechanism: on PostgreSQL 18.6 the
planner generates no index-only path for a query that filters on an
indexed expression, so the prescribed pair cannot remove the heap fetches
it exists for. Guard test: `holder_covering_index_decision_test.go`
(no-build pin); mechanism guard:
`TestHolderCoveringExpressionBlocksIndexOnlyLive`.

The #10564 section below is kept verbatim as the history of the first
no-build gate: its need analysis and cost census still stand, and the
re-trigger it named is what fired.

## New direction: adaptive splitting (no H-3 build, #10643)

The verdict above stands: the covering pair stays unbuilt and no DDL ships.
The guard pressure that re-fired the gate is now answered by splitting, not
by indexing: each 5-id holder-read chunk is partitioned by measured per-id
row mass (one probe statement per chunk, per-id equality arms over the same
serving indexes), hot ids (over 10,000 rows) run alone, cold ids pack at most
5 ids and 10,000 combined rows per statement, and the statements run with up
to 4 concurrent reads under the same 10 s guard. Threshold: 10,000 rows is
about 1/7 of the 67k-row statement that timed out at 9.8 s cold, and a
linear read of the single-hot-id shim worst case (~0.78 s) puts a 10k-row
singleton near 0.5 s, 4x under the 2 s per-statement bar. Parallelism bound:
holder reads are I/O-read dominated (72% of exec), one chunk splits into at
most 5 statements, and 4 concurrent reads keep one pass far under the pool
even with parallel workers. No statement of two or more ids covers more than
the threshold, so the timed-out shape cannot re-form; a singleton hot id may
exceed the threshold alone (residual: keyset paging, not more splitting).
Proof: `TestHolderAdaptiveSplitRowIdentityLive` (bidirectional EXCEPT 0/0
split-vs-unsplit from the shipped constant) and `TestHolderMassProbeLive`
(exact per-id masses, probe ~2.4 ms); full account in
`docs/internal/evidence/10643-adaptive-split.md`. Telemetry: no new counter
or label (the outcome set stays closed and mass is unbounded cardinality);
operators read `holder_statements`, `split_chunks`, `hot_ids`, and
`max_id_mass_rows` on the fan-out span, the summary log line, and the
`fanout_holder_statements`, `fanout_hot_ids`, `fanout_max_id_mass_rows`
sub-signals.

## Attempt: need (#10643, confirmed)

The #10564 re-trigger fired: per-statement holder fan-out reads on hot
CVEs pass ~5 s at production chunk size. Measured on the #10643 lane: 67k
candidate rows read ~9.8 s cold against the 10 s
`supplyChainImpactFanOutReadTimeout` guard. Every candidate row pays a heap
fetch for `scope_id`: the serving lookup pair carries `(expr,
impact_status, fact_id, generation_id)` but not `scope_id`, and the members
GIN arm can never go index-only. The stage-3 shim proof on #10643 bounds
the appetite: the verbatim query ran 9806 ms cold / ~560 ms warm, a
`generation_id`-only proxy ran ~170 ms (3.3x), and the visibility map
stands at 95.4%. The need is real; the prescribed fix is not, as the next
section proves.

## Attempt: widen-vs-net-new pricing (recorded, then moot)

Two shapes were priced against the 140M-row `fact_records` family before
the mechanism proof landed:

- **Widen the two existing lookup indexes in place** (add `scope_id` to
  `fact_records_supply_chain_impact_lookup_idx` and the advisory twin).
  Rejected on transition risk. Postgres cannot widen an index key without
  a rebuild, and migration 003 is immutable (#7002), so the same names
  cannot be redefined in place: the only in-place route is the
  guarded-drop machinery (the 128/129 pattern), which leaves a window with
  no usable index on the serving shape and adds replay-guard complexity.
  No shipped statement filters on `payload->>'impact_status'` against
  `fact_records` (verified by search: the column appears only in these
  index keys and in winner-store projections), so the rebuild would also
  churn a column with no filter consumer — but that saving does not
  outweigh the transition risk on the hot read path.
- **Net-new covering pair, keep the existing pair.** The old indexes stay
  untouched, so every serving plan keeps its current access path and the
  planner adopts the covering pair only where it wins; a rollback is one
  `DROP INDEX CONCURRENTLY` per name with no rebuild of anything else. No
  `replayRebuiltIndexNames` entry: the new names would be create-only,
  never dropped, so untracked replays converge. Prefix overlap is benign:
  both pairs lead with the same payload expressions, and the planner
  prices per query shape.

Cost, carried from the replica census below: one more partial index per
arm over finding rows only (~1.6 KB/entry with production-width scope
ids, about 5.5 GB at 2M finding rows, 6.5-11 GB for the pair); finding
writes would maintain two more btree entries plus WAL. The #10564 fixture
put the same shape at +69% for an identical 200k-row insert with 3 holder
indexes growing to 5. Moot now: no price is worth paying for a plan the
planner cannot generate (next section).

## Disproof: the expression blocks index-only (PG 18.6)

Proved on scratch PostgreSQL 18.6 with a faithful mock (60k wide rows,
100% `relallvisible`, btree on `(expr, scope, gen)` under the finding
predicate) and confirmed in-repo by
`TestHolderCoveringExpressionBlocksIndexOnlyLive`, which builds the
prescribed shape under test-local names on a hot-CVE/advisory fixture:

- Same index, same fully visible table: a query that does NOT reference
  the indexed expression plans `Index Only Scan ... Heap Fetches: 0`.
- The holder arm shape, which MUST reference it
  (`(payload->>'cve_id') = ANY(...)`, advisory twin likewise), plans
  `Bitmap Heap Scan ... Heap Blocks: exact=N` — with 2 matching rows as
  well as with hundreds, and identically for the `= ANY(array)` form.
- Widening the key with the predicate columns or dropping the partial
  predicate does not change this: the planner generates no index-only
  path while the expression is referenced, and falls back to a disabled
  plain Index Scan rather than an index-only scan when bitmap and plain
  scans are both disabled.

Mechanism: the expression's base Var (`payload`, the whole JSONB) is not
stored in the index, so the executor must visit the heap to evaluate the
filter. This is structural to the query shape, not a costing artifact:
estimates, visibility, and key width do not move it. Consequence: under
the bitmap plans the planner does choose, the pair is wider per entry
(~1.6 KB vs ~415 B) than the serving lookup indexes, so it would add
index-page I/O and write amplification for no read win. The pair was
removed from the lane before any migration shipped: no DDL ran on any
production table.

## No-regression evidence (#10643)

Row movement: none. No query text, index, migration, or bootstrap row
changed. The mechanism guard asserts the shipped query returns the same
ordered scope set as the frozen H0 baseline with the covering shape
present. The members GIN arm is untouched and keeps its bitmap heap
shape.

## Write amplification (#10643)

Unchanged: no index ships, so finding write amplification is unchanged.
Had the pair been built, every finding insert/update would carry two
~1.6 KB-key btree entries plus WAL, on top of the existing finding index
family (the #10564 fixture measured +69% on an identical 200k-row
insert).

## No-observability-change (#10643)

No metric, span, log, status row, or queue consumer added or altered. The
holder read stays on the existing closed site label (holders, in_flight,
manifest_generation, package, repository) of
`eshu_dp_supply_chain_impact_fanout_total`; no build-progress
observability applies because no build ships.

## Re-trigger (build requires a new mechanism)

Re-open H-3 only with a mechanism that removes the expression reference
from the arm's filter path, proved on a fixture first:

- A stored generated column (e.g. `payload->>'cve_id'` materialized) plus
  a plain-column covering index plus the query rewritten to it — priced
  with its own backfill/rewrite cost on the 140M-row family, not this
  record's pair cost; or
- A planner behavior change (verified on the pinned Postgres version)
  that generates index-only paths for expression-referencing quals — the
  mechanism guard's no-index-only assertions failing is exactly this
  signal, and the failing test names the re-price.

Either lane revises the guard test with its migration in the same change.

---
---

## History: no-build pricing gate (#10564, kept verbatim)

**Verdict: no-build.** The covering pair-arm indexes
`fact_records_sci_cve_scope_gen_idx` (`((payload->>'cve_id'), scope_id,
generation_id)`) and the advisory twin
`fact_records_sci_adv_scope_gen_idx` stay unbuilt: no read need at the
25-id design chunk, against a ~6.5-11 GB permanent pair cost plus one full
183 GB heap scan per build pass. No DDL ran on any production table; every
number below comes from read-only QA-replica probes or a disposable local
fixture. Guard test: `holder_covering_index_decision_test.go`.

### What was priced

The stage-3 shim named these two partial indexes the root fix for the CVE
arm's heap touches per worst-case chunk, with magnitude explicitly not
proven. This gate prices them on production-scale data before any DDL on
the 381 GB `fact_records` table, and builds only on proof of need and
worth.

### Need: not proven at the design chunk size

First-hand QA-replica (`eshu-postgresql-read-0`, replay lag 3.8 s)
read-only evidence, 2026-11-12, all planning-only `EXPLAIN` plus catalog
reads under `SET TRANSACTION READ ONLY` and a 30 s timeout:

- `fact_records` is 381 GB total / 183 GB heap / 131.9M rows; live
  finding rows are 1,972,948 across 1,834 scopes.
- The H3 pair-union shape takes the existing partial index per arm under a
  custom plan at both N=25 and N=50 (CVE arm: Index Scan on
  `fact_records_supply_chain_impact_lookup_idx`, est 2,338 / 3,802 rows;
  advisory arm: Index Scan on the advisory twin, est 1,187 / 2,374 rows;
  members arm: bitmap on the GIN index, est 13,192 rows). A generic-plan
  probe takes the same arms; that signal belongs to the H-2 lane.
- The shim's measured actuals (inspected record, not re-run here) put H3
  at N=20 at 1.34-1.54 s warm with 361k buffers: 6-7x headroom under the
  10 s guard. The 8.6 s / 1.27M-touch worst case exists only at N=200,
  which 25-id chunking removes with identical total work.

Two bounds keep this honest. The planner underestimates arm rows ~100x
(2.3k estimated vs ~200k actual at N=25), so any hypothetical-index
costing would inherit that error and cannot prove worth alone. And the
members arm is a GIN bitmap heap scan the proposal never touches.

### Worth: the pair costs ~6.5-11 GB permanent for a fraction of a second

- Finding-weighted `scope_id` averages 1,527 bytes (p50 1,773, sum 3.0 GB
  over 1.97M rows; one exact aggregate). The proposed key is ~1,632
  logical bytes/row. Calibrated by the sibling CVE lookup's observed
  density (787 MB for 1.99M rows, 1.68x over logical key bytes), each
  covering index lands near 5.5 GB, the pair near 11 GB (6.5 GB floor at
  perfect fill). That is 8-14x the 787 MB index it complements.
- Build price: `CREATE INDEX CONCURRENTLY` scans the 183 GB heap twice
  per index plus sorts 2M wide rows; no write block, but tens of minutes
  of heavy I/O per index on the primary, estimated, not measured (a real
  build is exactly what this gate forbids unproven).
- Write price, measured on a disposable PG 18.6 fixture with
  production-like 1.6 KB scope ids: an identical 200k-row finding INSERT
  took 5.13 s with the 3 existing holder indexes and 8.67 s with the 2
  covering indexes added (+69%; each wide covering costs about as much as
  all three existing combined). The table already maintains 8+ finding
  indexes per write; this adds two of the widest. Production write rate
  is NOT_CHECKED (replica stat counters read zero, freshly reset).
- Read benefit, same fixture: H3 at N=25 moved 883 ms to 863 ms (-2.3%);
  the planner kept Bitmap Heap Scan and never chose an index-only scan
  at that match fraction. On a write-heavy table most pages miss
  all-visible, so index-only degrades toward heap fetches anyway.

### Performance Evidence

Need side: replica plan shapes (custom, N=25/50) plus the shim's N=20
1.34 s warm actuals against the 10 s guard. Cost side: replica-measured
index/row/width census plus fixture-measured +69% insert delta and -2.3%
read delta. Fixture scale (200-400k rows) is not production scale; only
ratios and densities transfer, and the verdict rests on the
replica-measured widths and the absent guard pressure, not on fixture
wall time.

### No-Regression Evidence

Zero behavior change: no query text, index, migration, or bootstrap row
changed by this gate. The H3 shape and 25-id chunking it prices ride
with the main H implementation and its own differential tests.

### Write-Amplification

No new index exists, so write amplification is unchanged. Had the pair
been built, every finding insert/update would carry two ~1.6 KB-key
btree entries plus WAL, on top of the existing finding index family.

### No-Observability-Change

No metric, span, log, status row, or queue consumer added or altered.

### Evidence markers

No-Regression Evidence (#10564): H3 vs frozen-H0 differential returns identical ordered sets with bidirectional EXCEPT 0/0 under custom and generic plans plus the production pgx path (TestHolderH3DiffSetsLive).

Observability Evidence (#10564): failed holder fan-out reads emit the closed site label (holders, in_flight, manifest_generation, package, repository) on eshu_dp_supply_chain_impact_fanout_total; the telemetry coverage verifier agrees.

### Re-trigger

The re-trigger condition: reopen H-3 when H-1 shows per-statement p95
above ~5 s at production chunk size on primary-like load, or when
heap-read (not hit) buffers dominate a slow H3 statement. That lane then prices a real
`CONCURRENTLY` build with its own measured wall time, and revises the
guard test with the migration in the same change.
