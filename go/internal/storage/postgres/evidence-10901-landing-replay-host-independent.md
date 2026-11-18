# #10901 landing replay, host-independent half: evidence note

Lane: `drive/10901-landing-replay` in `/home/ubuntu/personal-repos/eshu-10901-landing-replay`
(base: origin/main at the #10895 merge). Scope is the host-independent half
ONLY: the quiet-host absolute re-prove and the sustained autovacuum soak
belong to a different host/soak and are explicitly out of scope here.

## Arm 1: distinct-key total-cost replay

Test: `TestSupplyChainImpactWinnersLandingDistinctTotalCostLive`
(`go/internal/storage/postgres/supply_chain_impact_winners_landing_distinct_total_cost_live_test.go`).

Same host, same run, two cells over the same mostly-distinct wave (2
writers, 25 statements each, 180 keys per statement: 162 fresh + 18
re-marks; 9,000 marks, 8,136 distinct keys per cell):

- direct: frozen pre-direction-C upsert (`hotkeyShimDirectUpsertSQL`).
- staging: production capture (`captureWinnersDirtyFactsSQL`) plus one
  `collapseWinnersStaging` tick transaction (GROUP BY merge-upsert plus the
  seq-bounded delete timed together, not a SELECT probe).

Pre-registered maximum regression bound (written in the test before
measuring): stagingTotal / directTotal <= 2.0. Rationale recorded in the
test: the staging cell performs the same logical inserts through a cheaper
statement (plain INSERT, no arbiter) plus exactly one extra full pass over
the wave (GROUP BY, merge, delete); a ratio above 2.0 means the collapse
costs more than the entire direct write path. The gate fails closed past
the bound. Conservation is asserted on growth for both cells (marks ==
log version growth; staging collapse groups/rows exact).

Measured on disposable Postgres (local PG 18.6, shared unowned host;
absolute times are notes, the same-run relative verdict travels):

- staging: writer_sum 126-127ms, collapse 57-60ms, total ~184-187ms.
- direct: writer_sum ~181-186ms.
- verdict ratio 1.005-1.016 against the 2.00 bound (two runs).
- conservation exact both cells: 9,000 marks == 9,000 version growth;
  collapse groups 8,136 rows 9,000; staging empty after each cell.

## Arm 2: multi-batch interleave vs collapse-in-tick

Test: `TestSupplyChainImpactWinnersLandingMultibatchInterleaveLive`
(`go/internal/storage/postgres/supply_chain_impact_winners_landing_multibatch_interleave_live_test.go`).

Production-shape writer transactions (mirroring
`commitSupplyChainImpactRows` in
`go/internal/reducer/supplychain/core/writer_retract.go`: advisory
conflict-domain lock, `factwrite.BatchInsertVersionedFacts` finding
upsert, production capture statement, production owed-marker publish
write, replace-set retract with fact_retract staging) interleave with a
collapse-in-tick loop (one transaction per collapse, no retry) at 2 and 4
writers over shared scopes (writers pair up on conflict domains, so the
advisory lock contends and same-scope retractions overlap while the
collapse drains underneath). Final bounded drain, then exact conservation
under the redefined identity (marks == drained version sums + residual
log versions + staging residual; residual is zero after the drain).

Measured:

- writers=2 scopes=1: marks 1,240, log version growth 1,240, log depth
  growth 136 over 136 distinct staged keys. Zero 40P01, no retry.
- writers=4 scopes=2: marks 2,480, log version growth 2,480, log depth
  growth 152 over 152 distinct staged keys. Zero 40P01, no retry.

## Incidental findings (lane-local, reported not shipped as fixes)

1. Bootstrap residue: bootstrap seeds fire a capture trigger and stage one
   row before any test writes. Both harnesses absorb it with one
   collapse before baselining and assert conservation on growth. The
   residue mechanism (trigger-fired seed row) is worth one line in the
   build PR evidence doc; the count (exactly 1 row here) is host/seed
   dependent and not asserted.
2. Lane-local build fix: `supply_chain_impact_winners_keyed_explain_export_test.go:90`
   returned 3 values from a 4-value function, so the whole postgres test
   package did not compile at this base. Fixed with the missing `""`
   (one line, test-only). Newer main refactored that function; drop this
   hunk on rebase.

## Unresolved (not proven here, by scope or by host)

- Quiet-host absolute re-prove with control canary (adjudication
  condition 3): needs the owned quiet host; explicitly out of scope.
- Sustained autovacuum soak with heap/dead-tuple evidence (adjudication
  condition 6 tail): needs the soak host; explicitly out of scope.
- `TestSupplyChainImpactWinnersResweepKeyedScopeExpansionPlanLive`
  (capacity lane #10839) fails on this host after the compile fix: the
  convert plan does not take the temp scope index (statistics-blind nested
  loop). The package never compiled at this base, so no working prior
  state regressed; the failure is in another lane's plan pins on a
  non-CI Postgres build. Left for the owning lane; not touched here.
