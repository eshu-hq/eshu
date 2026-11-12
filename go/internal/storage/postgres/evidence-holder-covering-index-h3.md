# H-3 covering-index pricing: documented no-build (#10564)

**Verdict: no-build.** The covering pair-arm indexes
`fact_records_sci_cve_scope_gen_idx` (`((payload->>'cve_id'), scope_id,
generation_id)`) and the advisory twin
`fact_records_sci_adv_scope_gen_idx` stay unbuilt: no read need at the
25-id design chunk, against a ~6.5-11 GB permanent pair cost plus one full
183 GB heap scan per build pass. No DDL ran on any production table; every
number below comes from read-only QA-replica probes or a disposable local
fixture. Guard test: `holder_covering_index_decision_test.go`.

## What was priced

The stage-3 shim named these two partial indexes the root fix for the CVE
arm's heap touches per worst-case chunk, with magnitude explicitly not
proven. This gate prices them on production-scale data before any DDL on
the 381 GB `fact_records` table, and builds only on proof of need and
worth.

## Need: not proven at the design chunk size

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

## Worth: the pair costs ~6.5-11 GB permanent for a fraction of a second

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

## Performance Evidence

Need side: replica plan shapes (custom, N=25/50) plus the shim's N=20
1.34 s warm actuals against the 10 s guard. Cost side: replica-measured
index/row/width census plus fixture-measured +69% insert delta and -2.3%
read delta. Fixture scale (200-400k rows) is not production scale; only
ratios and densities transfer, and the verdict rests on the
replica-measured widths and the absent guard pressure, not on fixture
wall time.

## No-Regression Evidence

Zero behavior change: no query text, index, migration, or bootstrap row
changed by this gate. The H3 shape and 25-id chunking it prices ride
with the main H implementation and its own differential tests.

## Write-Amplification

No new index exists, so write amplification is unchanged. Had the pair
been built, every finding insert/update would carry two ~1.6 KB-key
btree entries plus WAL, on top of the existing finding index family.

## No-Observability-Change

No metric, span, log, status row, or queue consumer added or altered.

## Re-trigger

The re-trigger condition: reopen H-3 when H-1 shows per-statement p95
above ~5 s at production chunk size on primary-like load, or when
heap-read (not hit) buffers dominate a slow H3 statement. That lane then prices a real
`CONCURRENTLY` build with its own measured wall time, and revises the
guard test with the migration in the same change.
