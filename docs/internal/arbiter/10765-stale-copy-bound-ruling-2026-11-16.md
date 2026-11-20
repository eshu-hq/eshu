# Arbiter ruling: stale active copy outranks a retracted copy (#10765)

Date: 2026-11-16. Refs #10765. Arbiter: a separate-context deepest-tier model
given the issue's three options and the state of `main` after PR #10772
(merged at b0ee7754, 2026-11-16T08:46Z). The coordinator re-checked every path
and line below against the tree before recording this file.

## Decision

**The documented bound is accepted and no code changes.** A stale active copy in
a holder scope can outrank the retracted copy in the default findings view until
that holder re-derives. The window is bounded by the one-hop holder fan-out, one
holder pass, and one winners resweep tick, and the HTTP API page says so.

## Options

| Option | Ruling | Why |
|---|---|---|
| Documented bound | Accepted | The fan-out on `main` already reopens every holder scope for the changed ids, up to 256 holders per claim with the remainder carried to the next claim. |
| Retraction-aware tiebreak in the winners order | Rejected | It needs a new winners column, a rewrite of the four priority indexes, and a new keyset key, which is an API cursor change. It is also wrong in the restore direction: a stale retracted copy would hide a restored, real vulnerability. Accuracy loses either way; the bound loses nothing. |
| Bounded re-derive trigger | Rejected | It duplicates the fan-out that already reopens the holders. |

## Remaining-copies telemetry

Acceptance bullet 3 asks for telemetry on how many stale active copies remain.
`eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}` counts
retraction-state flips, not copies still stale, and its description says so.
No gauge is added. The remaining count is derivable from instruments already on
`main`: holders the fan-out reopened or deferred for cause `advisory`, minus the
`active_to_retracted` flips over the same window. A direct per-pass winners scan
would add an unmeasured read to a hot path for a window that is already
bounded. The derivation is written in the telemetry reference page below.

The figure is an estimate. A holder deferred in one claim and reopened in a
later one inside the window is counted under both outcomes, and the counter
includes the originating scope's own flip.

## Verified evidence

Checked on `origin/main` at 2d8328d60b.

- Winners ordering is unchanged: `priority_score DESC`, `has_payload_finding_id DESC`,
  `fact_id ASC` at `go/internal/storage/postgres/supply_chain_impact_canonical_winners_store.go` (the winners select's `ORDER BY`).
- The reproduction is a live test:
  `TestSupplyChainImpactWinnersResweepStaleActiveCopyOutranksRetractedCopyLive`
  at `go/internal/storage/postgres/supply_chain_impact_winners_stale_copy_pin_live_test.go`,
  listed in `scripts/lib/reducer_contention_inventory.json` and
  `specs/live-tests.d/live-tests-07.yaml`.
- The cap is `fanOutHolderCap = 256` at `go/internal/reducer/supplychain/core/fanout.go`.
- The fan-out counter `eshu_dp_supply_chain_impact_fanout_total{domain,outcome,cause}`
  is documented at `go/internal/telemetry/instruments.go` (the `SupplyChainImpactFanOut` field doc) and
  registered in the same file. Its outcomes include
  `reopened` and `deferred`; `deferred` is added once per newly carried holder
  (`go/internal/reducer/supplychain/core/fanout_signals.go`).
- The repair counter is `eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}`
  in `go/internal/telemetry/instruments_supply_chain_impact_winners.go`, with the
  transition value `active_to_retracted`. Its evidence note is
  (#10765).
- The bound text is in `docs/public/reference/http-api/vulnerability-impact.md:209`
  and the "Retraction freshness" section of
  `docs/public/reference/telemetry/supply-chain-impact-winners.md`.

## What this ruling does not do

- Change the winners ordering, any index, the migration set, or the API cursor.
- Add a gauge or a per-pass scan for the remaining stale copies.
- Add a second re-derive trigger beside the holder fan-out.
