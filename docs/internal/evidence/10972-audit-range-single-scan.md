# #10972 staged audit: single-scan range renderer with canonical ident index

Refs #10972. Read-path-only change: a new renderer for the staged-audit
range statement plus migration 356. No write, chunk, lease, clock, or
tripwire path is touched. The new statements are not wired into the sweep
yet; the shipped pair keeps serving.

## What changed

- `go/internal/storage/postgres/supply_chain_impact_winners_audit_range_single_scan.go`:
  renders the range statement with `range_raw` narrowed to
  `(fact_id, scope_id, generation_id)` plus the five 291 identifier
  expressions (byte-identical to migration 291), one MATERIALIZED
  `range_bounds` folded from tail/below-tail/count with single-pass window
  functions, and the non-oversize `range_facts` arm re-attaching
  `payload`/`source_confidence` by PK join in the same statement and
  snapshot. The oversize arm is textually unchanged. Also owns
  `StagedAuditRangeObservation` with `report()` and `ObserveRange`
  (raw/facts over-fetch ratio), currently unwired.
- `go/internal/storage/postgres/migrations/356_supply_chain_impact_winners_canonical_ident_idx.sql`:
  `CREATE INDEX CONCURRENTLY` on `supply_chain_impact_canonical_winners`
  over the five stored identifier expressions, byte-identical to
  `winnersAuditRangeStoredIdentExprs` after stripping the `w.` qualifier.
- Tests: hermetic shape pins, a T3-style lockstep differential at budgets
  1, 7, 50, 200, 1000 and 100000, a NULL-tail knife-edge proof, a
  buffer-parity cost proof, and a stored-index EXPLAIN pin.

## No-Regression Evidence: #10972 single-scan range

The differential
(`TestSupplyChainImpactWinnersResweepSingleScanUnionEqualsShippedLive`)
walks the shipped and single-scan pairs in lockstep over the #11131
fixture with seeded stale, missing and orphan winners: per-range RawRows,
Facts, Last, Oversize, Pipe, Hi and candidate sets are identical, the
union equals the shipped single-statement candidate set, and every live
fact is recomputed exactly once. The RED pair failed to compile before the
renderer existed. The oversize re-scan is pinned textually unchanged and
the 291 identifier expressions byte-identical
(`TestSingleScanOversizeArmTextuallyUnchanged`,
`TestSingleScanIdentExprsMatchMigration291`). The knife-edge proof
(`TestSupplyChainImpactWinnersResweepSingleScanNullTailKnifeEdgeLive`)
pins the one divergence: at budget exactly equal to the corpus, the NULL
identifier row sorts first under ORDER BY DESC, the shipped pair takes the
oversize arm with NULL bounds and errors (no upper bound), and the
single-scan pair completes with the same candidate union. Under a clean
NULL probe (which gates every sweep) `range_raw` carries no NULL
identifiers and both pairs agree.

## Performance Evidence: single-scan vs shipped buffers

Cost proof
(`TestSupplyChainImpactWinnersResweepSingleScanCostLive`, local Postgres
18.6, padded 1500-identifier fixture, EXPLAIN ANALYZE BUFFERS FORMAT JSON
root-node totals): single-scan vs shipped totals are 143,443 vs 141,457 at
budget 2000 (+1.4%) and 777,141 vs 775,218 at budget 10000 (+0.25%), with
three stable reps and execution time +5% (75.6ms vs 79.6ms at budget 2000).
The <1/2 premise does not hold here: heap fetches pull the full tuple
whatever the select list projects, so the narrow scan saves no heap
buffers, while the PK re-attachment adds about one probe per kept row. The
test pins parity (single-scan within 1.25x of shipped) and logs both
plans; any claim built on a <1/2 bound belongs to a QA-scale spill regime
this fixture does not reproduce and needs an arbiter ruling first. The
stored-side win is proven separately: the range's stored side rides the new
index (Index Scan using
`supply_chain_impact_winners_canonical_ident_idx`, no Seq Scan).

## No-Observability-Change: no new production telemetry

This change adds the `StagedAuditRangeObservation` type with its `report()`
and `ObserveRange` constructors, but wires no production caller and adds no
metric instrument, metric label, span name, log line, worker, lease, queue
domain, route, or runtime knob. Operators diagnose the range path through
the existing `InstrumentedDB` Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`, exactly as for the shipped range
statement.
