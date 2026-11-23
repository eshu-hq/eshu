# Keyed winners chunk bound: 1000 tried and reverted, stays 200 (#10839)

`defaultWinnersChunkMaxIdents` stays at 200. A 1000-identifier default (#11295)
was merged and then reverted after the first QA tick ran far slower than the
model predicted. This note records the measurement; it does not recommend 1000.

## Measured QA result

One tick at chunk 1000, under the 338 supports backfill:

- Wall time: 710.8 s for 237,063 kept rows across 4,027 identifiers.
- Model (4 s fixed + 1.63 ms per kept row): about 390 s. Measured is 1.8x that.
- Chunk 200 control ticks: 65-72 s for 34-49k kept rows, so the per-kept-row
  cost at 1000 was about 1.8x the cost at 200.
- Rolled back at 2026-11-23T15:01Z (QA returned to 0772ee6913). The result is
  posted as a comment on #10839.

This is one sample, taken while the 338 supports backfill ran. The cause is not
shown and was not isolated.

## What the earlier replay did and did not show

The pre-merge Decision-5 replay (disposable PG18, 5000-row window) drained 2.30x
faster at 1000 than at 200. That replay did not predict the QA per-kept-row
cost, and the reason is not shown here, so it is not evidence that 1000 is safe
at QA scale. Chunk tuning is not the lever at QA depth (arbiter ruling of
2026-11-23, recorded in the goal file): the next lever is the cost per kept row
or the number of kept rows per identifier. Any future attempt to raise the bound
needs a QA-shaped measurement (kept rows per tick in the 200k range) and a new
ruling before it lands.

## Performance Evidence (no regression):

The revert restores the deployed 0772ee6913 behaviour: one default constant back
to 200, no SQL, lock, worker, batch, window, cadence, or clamp change (#11157
stays 13,107; 200 x 5 = 1000 INSERT params).

## No-Regression Evidence:

The diff touches no SQL and no settle predicate, so the sound S/H horizon,
oldest-first chunk order, exact-applied delete, and scope-remainder conversion
ordering are unchanged. Contract proof: `TestWinnersChunkDefaultBoundIs200`
(failing-first: RED `{identifiers:1000}` before the revert, GREEN after) pins
the 200 default, the parameter-ceiling headroom, and a saturated default-bound
tick (200 admitted, truncated, one INSERT). The #10839 batch tests and the
#11157 clamp tests, including the #11302 derived clamp assertion, pass
unchanged.

## Observability Evidence (revert adds no telemetry):

No new spans or metrics. The existing `BuildActiveFacts` tick-phase histogram,
the per-tick statement count, and
`eshu_dp_supply_chain_impact_winners_chunked_ticks_total` show tick size and
duration. The public telemetry page states the 200-identifier bound again.
