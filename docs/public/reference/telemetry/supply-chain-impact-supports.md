# Supply-Chain Impact Supports

The per-support table `supply_chain_impact_finding_supports` holds one row per
live finding fact (#10880 PR-B1). The impact writer keeps it current in the same
transaction that writes the finding facts, and a reducer-owned backfill fills it
for facts written before the maintainer shipped. No reader serves from it yet;
these signals show whether it is complete and current before a reader does.

| Signal | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_supports_rows_total` | counter | `action`, `source` | Support rows written. `action` is `inserted`, `updated`, `unchanged` (the write-on-change guard left a replayed finding alone) or `deleted` (the row followed a retracted fact). `source` is `writer` (counted after the writer's transaction commits, so a rollback adds nothing) or `backfill`. A `writer` series with `inserted` and `updated` flat while findings are written is a maintainer that stopped. |
| `eshu_dp_supply_chain_impact_supports_backfill_steps_total` | counter | `outcome` | Backfill steps: `chunk` (supports written or orphans removed for one scope and generation), `verify_gap` (the verification found live findings without a support and restarted the sweep), `complete` (the step that set the readiness marker), `idle` (a step after completion), `contended` (another reducer held the lock) and `failed`. A rising `verify_gap` means supports go missing after the sweep passed them, which only a writer without the maintainer causes. |
| `eshu_dp_supply_chain_impact_supports_backfill_ready` | gauge | none | 1 once the backfill finished and verified at the current contract version, else 0. A reader of the table refuses a grant-scoped call while it is 0. |

No label carries a scope, repository, canonical key or CVE.

## Reading them

- Right after a deploy the gauge reads 0 and `chunk` steps run about twice a
  second. The gauge reaches 1 with one `complete` step. `idle` steps then
  appear every five minutes, one marker read each.
- The marker is set only after a verification pass found no live finding fact
  without a support. A `verify_gap` step moves the cursor back to the start; the
  next sweep fills the gap before the marker is set.
- The readiness predicate is `SupplyChainImpactSupportsReady`: the marker row
  has `completed_at` set and a `contract_version` at least the current
  `SupplyChainImpactSupportsContractVersion`. A missing table reads as an error,
  and a reader treats it as not ready.
- The backfill takes each scope and generation's conflict-domain advisory lock
  (the impact writer's lock) for one chunk of at most 500 supports, so a writer
  waits for at most one chunk.
