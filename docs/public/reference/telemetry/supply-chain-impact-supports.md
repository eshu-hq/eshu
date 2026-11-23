# Supply-Chain Impact Supports

The per-support table `supply_chain_impact_finding_supports` holds one row per
live finding fact (#10880 PR-B1). The impact writer keeps it current in the same
transaction that writes the finding facts, and a reducer-owned backfill fills it
for facts written before the maintainer shipped. An old-writer fence marks any
scope a writer without the maintainer touches, and the backfill drains those
marks. With `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` on, the scoped-token list,
count, inventory, grouped inventory, explain, and the repository, service, and
workload anchored advisory evidence reads serve from it (#10880 PR-B2a) and answer
`503 backend_unavailable` with `Retry-After: 30` while it is not ready. These
signals show whether it is complete and current, which is the same predicate those
reads probe before each call.

| Signal | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_supports_rows_total` | counter | `action`, `source` | Support rows written. `action` is `inserted`, `updated`, `unchanged` (the write-on-change guard left a replayed finding alone) or `deleted` (the row followed a retracted fact). `source` is `writer` (counted after the writer's transaction commits, so a rollback adds nothing) or `backfill`. The backfill reports one count for supports it inserted or changed, so `source=backfill` counts both under `inserted` and never emits `updated`. A `writer` series with `inserted` and `updated` flat while findings are written is a maintainer that stopped. |
| `eshu_dp_supply_chain_impact_supports_backfill_steps_total` | counter | `outcome` | Backfill steps: `chunk` (supports written or orphans removed for one scope and generation), `drain` (a scope the old-writer fence marked dirty was re-read), `verify_gap` (the verification found live findings without a support and restarted the sweep), `complete` (the step that set the readiness marker), `idle` (a step after completion with nothing to drain), `contended` (another reducer held the lock) and `failed`. A rising `verify_gap` means supports went missing after the sweep passed them. |
| `eshu_dp_supply_chain_impact_supports_backfill_step_duration_seconds` | histogram | `outcome` | Wall time of one backfill step. A step holds one scope-and-generation advisory lock for at most one chunk of 500 supports, so the tail is the longest a writer can wait on the backfill. |
| `eshu_dp_supply_chain_impact_supports_dirty_scopes` | gauge | none | Scopes the old-writer fence marked dirty that the backfill has not drained, capped at 1000. Non-zero holds the table not ready. A count that never falls means a writer without the maintainer is still running. |
| `eshu_dp_supply_chain_impact_supports_backfill_last_success_timestamp_seconds` | gauge | none | Unix time of the last backfill step that finished without error. A value that stops moving while the gauge below reads 0 means the loop is stuck. |
| `eshu_dp_supply_chain_impact_supports_backfill_ready` | gauge | none | 1 once the backfill finished and verified at the current contract version and the fence is empty, else 0. A reader of the table refuses a grant-scoped call while it is 0. |

No label carries a scope, repository, canonical key or CVE.

## Reading them

- Right after a deploy the gauge reads 0 and `chunk` steps run about twice a
  second. The gauge reaches 1 with one `complete` step. `idle` steps then
  appear every 30 seconds, one marker read and one fence check each.
- During a rolling upgrade, reducers of the previous release still write finding
  facts without the maintainer. Each such write marks its scope dirty (the dirty
  gauge rises) and the table reads not ready; the backfill drains the marks
  (`drain` steps) and the gauge returns to 1 once the old reducers are gone.
- The marker is set only after a verification pass found no live finding fact
  without a support. A `verify_gap` step moves the cursor back to the start; the
  next sweep fills the gap before the marker is set.
- The readiness predicate is `SupplyChainImpactSupportsReady`: the marker row
  `supply_chain_impact_finding_supports_v<version>` has `completed_at` set, no
  marker for a higher version exists (a newer binary ran the sweep), and the
  dirty-scope table is empty. A missing table reads as an error, and a reader
  treats it as not ready.
- The backfill takes each scope and generation's conflict-domain advisory lock
  (the impact writer's lock) for one chunk of at most 500 supports, so a writer
  waits for at most one chunk.

## Least-privilege logins

Migration 338 creates the tables and grants nothing. A database that uses
least-privilege writer logins must grant the reducer login, after the schema
migration that creates the tables and before the new reducer writes impact
findings:

- `SELECT`, `INSERT`, `UPDATE` and `DELETE` on
  `supply_chain_impact_finding_supports`, which the impact writer upserts and
  retracts and the backfill fills.
- `SELECT`, `INSERT` and `UPDATE` on
  `supply_chain_impact_finding_supports_backfill`, the marker and cursor table.
- `SELECT` and `DELETE` on `supply_chain_impact_finding_supports_dirty`, which
  the backfill drain reads and clears.

None of the three tables has a sequence. Until the first grant exists, the new
reducer's impact writes fail with `permission denied for table
supply_chain_impact_finding_supports` (SQLSTATE 42501), and each pass rolls back
and retries.

The old-writer fence trigger needs no grant for any login. Its function runs as
its owner (`SECURITY DEFINER`, with a search_path pinned to `pg_catalog`, its own
schema and `pg_temp`, and PUBLIC execute revoked), the same form as the winners
capture functions. A reducer of the previous release that writes finding facts
through a least-privilege login still marks its scope dirty during a rolling
upgrade, with no privilege on the dirty table. This is proven by live tests
against a login that holds `INSERT` and `UPDATE` on `fact_records` and nothing on
the dirty table. It is not proven on a deployed least-privilege database.
