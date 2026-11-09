# Advisory Group Projection

The `advisory_group_projection` reducer domain projects the supply-chain
impact canonical winners read model into the graph: one thin
`AdvisoryGroup` anchor node per advisory group (a CVE and the advisory ids
grouped with it), linked from each affected `Repository` by an existence-only
`AFFECTED_BY` edge (#10411). The Postgres winners stay authoritative; the
graph family is a cache that converges group by group.

## How a projection runs

A `supply_chain_impact` pass reports the advisory groups its committed
findings touched. After the commit and before the holder fan-out, the
handler admits one `advisory_group_projection` intent per touched group.
A failed admission fails the pass for queue retry while its owed marker
still stands, so the owed rerun re-enqueues the scope's recovery groups.

The projection handler first waits for the winners watermark to pass the
intent's enqueue time. The winners maintainer rebuilds on a 30 second
cadence, so an intent claimed right after the touching pass committed would
otherwise project the pre-pass winners and acknowledge. While the watermark
is behind, the handler defers in the non-counting failure class
`advisory_group_winners_watermark_not_ready`, recorded as deferred, not
failed. Past a 30 minute bound the wait reclassifies to the counting class
`advisory_group_winners_watermark_stale` and the intent can dead-letter: a
watermark stuck that long means the maintainer is down, and the
[Supply-Chain Impact Winners Resweep](supply-chain-impact-winners.md)
signals say why.

At a fresh watermark the handler reads the group's qualifying winners,
folds them into the desired node membership and edge set, and reconciles
the graph to exactly that state. A group with no qualifying winner
reconciles to no node at all.

## Signals

Every label is `domain` or a closed `outcome`; no label carries a group,
CVE, advisory, repository, or package id.

| Metric | Meaning |
| --- | --- |
| `eshu_dp_advisory_group_projection_groups_total{domain,outcome}` | Groups reconciled by outcome: `projected` (node and edges written), `deleted` (no winner qualified, node and edges retracted), `edgeless_with_winners` (winners qualified but resolved to no (repository, package) pair, so the anchor was deleted), `failed` (reconcile errored; the intent retries). |
| `eshu_dp_advisory_group_projection_edges_upserted_total{domain}` | `AFFECTED_BY` edges created. Compare with qualifying-winner volume when the graph looks thin. |
| `eshu_dp_advisory_group_projection_edges_retracted_total{domain}` | `AFFECTED_BY` edges deleted because their winner stopped qualifying (fixed, suppressed, or retracted upstream). |
| `eshu_dp_advisory_group_projection_nodes_deleted_total{domain}` | `AdvisoryGroup` nodes deleted because the group's winner set emptied. |
| `eshu_dp_advisory_group_projection_member_union_oversized_total{domain}` | Groups whose member union exceeded 32 ids. The group still projects in full; a steady rate means alias grouping is producing unusually wide groups worth checking against the allow/deny lists. |
| `eshu_dp_advisory_group_projection_enqueue_failures_total{domain}` | `supply_chain_impact` passes whose post-commit projection enqueue failed. The pass fails retryable before its owed marker clears, so the owed rerun re-enqueues the scope's recovery groups; a non-zero rate means enqueue trouble is delaying convergence. |
| `eshu_dp_advisory_group_projection_reconcile_duration_seconds{domain}` | One group's reconcile wall time (winner read plus graph write). |
| `eshu_dp_advisory_group_projection_lag_seconds{domain}` | Intent enqueue-to-completion seconds, including the watermark wait. It bounds how long the graph trails the winners read model for a touched group; a rising tail with a fresh watermark points at queue depth, not the maintainer. |

The INFO log `advisory group projection completed` carries counts only
(`group_count`, `winner_row_count`, `edges_created`, `edges_deleted`,
`nodes_deleted`, `oversized_member_union_count`), scoped by `scope_id` and
`generation_id`; find one group's story through its winners rows, not the
log. A failed enqueue increments the enqueue-failures counter above and
fails the pass for queue retry. The domain runs
as a standard reducer execution, so `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds` cover its queue lifecycle.
