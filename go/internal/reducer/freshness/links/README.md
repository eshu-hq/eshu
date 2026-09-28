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
| `linked` | A root or incremental link committed. | Advanced to the linked generation; attempt fields and the poison marker clear. |
| `break` | A delta generation, a pruned generation, or a delta whose prior is unknown. The overlay link is not shipped, so every delta activation is a break (`delta_without_root`, `prior_mismatch`, `overlay_unproven`, `pruned_before_link`). | Advanced; the state is kept. |
| non-counting miss | `cursor_locked`, `generation_locked`, `slot_busy`. The runner moves on to the next candidate. | Unchanged; nothing written. |
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
- `eshu_dp_changed_since_chain_breaks_total{reason}`
- `eshu_dp_changed_since_link_duration_seconds{link_kind}`
- `eshu_dp_changed_since_link_delta_rows{link_kind}`
- `eshu_dp_changed_since_link_keys{link_kind}`
- `eshu_dp_changed_since_link_backlog`
- `eshu_dp_changed_since_link_lag_seconds`
- `eshu_dp_changed_since_state_bytes`
- `eshu_dp_changed_since_state_rows`
- `eshu_dp_changed_since_deltas_bytes`, `eshu_dp_changed_since_deltas_rows`
  (the link-delta table. Generation retention bounds it, apart from at most
  one link per scope whose prior was pruned; see the ledger store's README)
- `eshu_dp_changed_since_link_retrying_scopes`, `eshu_dp_changed_since_link_poisoned_scopes` (computed in SQL, fleet-wide)
- One ERROR `changed-since link poisoned` log per poisoning, with scope,
  generation, sequence, failure class and attempts.
- The span `reducer.changed_since_link` covers each link and carries the
  scope, generation, prior, sequence, kind and break reason.
- One `changed-since link` log line is written per committed link.
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
  generation.
