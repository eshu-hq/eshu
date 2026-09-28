# changed_since_link reducer domain

## Purpose

This package runs the dark `changed_since_link` domain of #7127 PR-3a. It keeps
the changed-since ledger (migration 136) current: every generation activation
becomes a link, meaning the set of keys that changed since the scope's
previously linked generation. The read path that serves `get_changed_since`
from the ledger lands in PR-3c. Until then nothing reads these tables.

## Runtime

`cmd/reducer` builds a `Runner` only when
`ESHU_CHANGED_SINCE_LINK_ENABLED=true` (default `false`) and starts it as a
side runner next to generation retention. With the switch off, the runner is
nil and the domain issues no SQL.

Each cycle does the following:

1. It journals activations. `JournalStore.Journal` backfills retained chains
   for scopes that have no journal rows (bounded by
   `ESHU_CHANGED_SINCE_LINK_BACKFILL_SCOPES_PER_CYCLE`). It then adds a
   `sweeper` row with an unknown prior for every active generation that has
   none. The pass is exclusive across replicas through a try-only advisory
   lock.
2. It deletes the ledger rows of scopes that no longer exist.
3. It links the backlog. Up to `ESHU_CHANGED_SINCE_LINK_WORKERS` scopes run at
   once, and each scope drains up to 16 activations per cycle. A retry or a
   failure stops that scope until the next cycle.
4. It samples the gauges.

Full links (root and incremental) also need one of
`ESHU_CHANGED_SINCE_LINK_SLOTS` database-wide advisory slots (default 2).
Each full link runs at `work_mem` 256MB under
`ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT` (default 120s). The poll
interval is `ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL` (default 5s).

## Outcomes

| Outcome | Meaning | Cursor |
| --- | --- | --- |
| `linked` | A root or incremental link committed, or a rebase: the state's generation was pruned, so the state moved to the activating generation by the same diff and a `root` link was recorded, with one `prior_pruned` chain break counted (arbiter ruling arb-7127-3d, C2). | Advanced to the linked generation; attempt fields and the poison marker clear. |
| `break` | A delta generation, a pruned generation, or a delta whose prior is unknown. The overlay link is not shipped, so every delta activation is a break (`delta_without_root`, `prior_mismatch`, `overlay_unproven`, `pruned_before_link`). | Advanced; the state is kept. |
| non-counting miss | `cursor_locked`, `generation_locked` (retention holds the activating generation or the link's prior), `slot_busy`. The runner moves on to the next candidate. | Unchanged; nothing written. |
| `failed` | A counting failure: `statement_timeout`, `connection_lost`, `sql_error`, `internal`. Recorded with backoff min(30 min, 30 s × 2^(n-1)). | Unchanged; `attempt_count` +1, `next_attempt_at` set. |
| `canceled` | The reducer shut down while the link ran or while its failure was being recorded (the runner's own context ended). Logged at INFO, with the failure class when one was observed; no ERROR. A failure or poisoning already recorded on the cursor keeps its ERROR. The link's own transaction deadline or statement timeout is not this: it is `failed`. | Unchanged; nothing recorded; the next cycle retries. |
| `poisoned` | The `ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS`-th counting failure (default 5): a `link_poisoned` break. | Advanced past the activation; state kept; marker set until the next full link. |

A poisoned link does not appear in `list_dead_letter_work_items` or the
status surface; the cursor row is the durable record (#7127 ruling 8.10,
known gap).

## Telemetry

- `eshu_dp_changed_since_links_total{link_kind, outcome}`
- `eshu_dp_changed_since_link_retries_total{reason}` (non-counting misses)
- `eshu_dp_changed_since_link_failures_total{failure_class}`
- `eshu_dp_changed_since_chain_breaks_total{reason}` (`prior_pruned` is a
  rebase: it comes with a `root` link, not instead of one)
- `eshu_dp_changed_since_link_duration_seconds{link_kind}`
- `eshu_dp_changed_since_link_delta_rows{link_kind}`
- `eshu_dp_changed_since_link_keys{link_kind}`
- `eshu_dp_changed_since_link_backlog`
- `eshu_dp_changed_since_link_lag_seconds`
- `eshu_dp_changed_since_state_bytes`
- `eshu_dp_changed_since_state_rows`
- `eshu_dp_changed_since_deltas_bytes`, `eshu_dp_changed_since_deltas_rows`
  (the link-delta table, which generation retention bounds; see the ledger
  store's README)
- `eshu_dp_changed_since_ledger_orphans{kind}` (`link`, `activation`,
  `bucket_group`): the orphan probe, sampled at most once a minute because it
  scans the link, activation and bucket-count tables (about 100 ms at 25,000
  links). Every ledger writer fences the generations it names, so it stays at
  zero; a deleted scope shows briefly until the orphan purge
- `eshu_dp_changed_since_link_retrying_scopes`, `eshu_dp_changed_since_link_poisoned_scopes` (computed in SQL, fleet-wide)
- One ERROR `changed-since link poisoned` log per poisoning, with scope,
  generation, sequence, failure class and attempts.
- The span `reducer.changed_since_link` covers each link and carries the
  scope, generation, prior, sequence, kind, break reason and, for a rebase,
  `changed_since.rebased_from_generation_id`.
- One `changed-since link` log line is written per committed link; a rebase
  carries `break_reason=prior_pruned` and `rebased_from_generation_id`.
- `changed-since ledger orphan probe failed` (ERROR) when the probe read
  fails.
- One `changed-since link cycle completed` log line is written per non-idle
  cycle.

No metric label carries a scope or generation identifier.

At 3 AM, read the signals this way:
- A rising backlog and lag mean the writer is behind.
- `retries_total{reason="slot_busy"}` means the full-link slots are
  saturated.
- `retrying_scopes` above zero means links are failing now;
  `poisoned_scopes` above zero means those scopes skipped an activation and
  answer from the fallback until their next full generation.
- `chain_breaks_total` says why a scope's state stopped following its active
  generation. `reason="prior_pruned"` is the exception: the state did follow
  (a rebase), but the link from the pruned generation is not recorded.
- `ledger_orphans` above zero for more than a sample or two means a ledger
  writer wrote a row naming a generation it did not lock: a code defect
  (arbiter ruling arb-7127-3d, C4), not load.
