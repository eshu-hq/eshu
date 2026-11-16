# A stale active copy can outrank a retracted copy: bound and counter (#10765)

The default findings view keeps one winner per advisory group and orders by
score first. A holder scope that has not re-derived since the advisory was
retracted still stores an active copy at its old score, so it can outrank the
re-derived retracted copy in another scope until that scope rewrites. The
holder fan-out already reopens every holder scope when a retraction lands, so
the window is bounded by the fan-out and by the next pass of the stale scope.
This change adds no ordering, index, schema or trigger. It pins the behavior
and counts the repairs, so an operator can see the window close.

## What changed at runtime

- `listStoredSupplyChainImpactFindingsQuery` reads one more column,
  `COALESCE(NULLIF(payload ->> 'suppression_state', ''), 'active')`, from the
  rows it already reads for change detection. No new table, join, scan, lock or
  round trip.
- `staleCopyRepairsOf` compares each rewritten row's new state with the state
  stored for the same fact id (or the previous generation's row by stable key on
  a first pass). It is one pass over rows already in memory; new and retired
  rows are not counted.
- The handler adds the result to
  `eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}` after the
  writer commits, at most twice per pass (one add per transition).

## Performance Evidence

No benchmark was run. The cost is argued from the code above and was not
measured: one extra JSONB text extract per stored row the query already
returns, one in-memory loop over those rows, and at most two counter adds per
pass. Backend and version for the behavior proofs: PostgreSQL 18.6, a throwaway
local database. Input shape in those proofs: two holder scopes, one advisory
group, so the row counts are single digits and say nothing about scale.
Terminal count: the writer live proof asserts
`StaleCopyRepairs{ActiveToRetracted: 1}` from the real SQL and the real writer.
The `RetractedToActive` direction is covered by the unit tests, not by a live
database run.

No-Regression Evidence: the winners ordering, indexes (migration 033), keyset
paging and the write path's statements are unchanged, so the work per pass is
the same set of statements over the same rows plus the extra extract. If a pass
over a very large scope is ever shown to slow, the extra column is the first
place to look; no such measurement exists today.

## Observability Evidence

Observability Evidence: an operator at 3 AM reads three signals, all bounded:

- Metric `eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}`,
  with the closed set `active_to_retracted` and `retracted_to_active`. No domain
  label and no per-id label. A sustained non-zero `retracted_to_active` rate
  means retractions are being undone; a burst of `active_to_retracted` after a
  retraction is the window closing.
- The reducer success line carries `sub_signal_stale_copy_repairs_active_to_retracted`
  and `sub_signal_stale_copy_repairs_retracted_to_active` for the same pass.
- A rolled-back pass counts nothing, because the count is emitted only after the
  writer commits and the writer returns no error. A pass that commits and then
  fails in the projection enqueue or fan-out still reports its count, so a retry
  cannot lose it.

`docs/public/reference/telemetry/supply-chain-impact-winners.md` has the metric
row and a "Retraction freshness" section.

## What the proofs cover

- A live pin against the real writer and winners select shows a stale active
  copy at score 100 beating a retracted copy at 40, the lower `fact_id` winning
  at equal scores, and the retracted copy winning after the stale scope
  rewrites. It is a characterization pin: it passed on first run. Its
  sensitivity was shown by temporarily putting
  `(suppression_state = 'advisory_retracted') DESC` first in the winners
  `ORDER BY`, which failed two subtests; that change was reverted and never
  committed.
- Unit tests fail when the emit call is removed, when the stored-state column is
  not read, and when the baseline is switched to the previous generation only.

## Not checked

- Behavior at scale: no benchmark against a QA-sized scope.
- The live pin ran with a local database, not the hosted live-postgres lane at
  the time of writing.
