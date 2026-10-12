# Id-Anchor Census

On Neo4j the reducer publishes `eshu_dp_graph_id_anchor_unreachable_nodes`, a
snapshot of graph nodes that carry an `id` the labeled entity-context anchor
cannot reach. The anchor reaches a node through an id-constrained label, or
through a uid-constrained label with `uid` equal to `id`. Zero is the
invariant. Since #7212 the Neo4j read of
`GET /api/v0/entities/{entity_id}/context` (and MCP `get_entity_context`) sends
only that anchor, with no unlabeled whole-graph scan after it. A node the anchor
cannot reach answers not-found there, so a nonzero count is a number of entity
ids that the entity-context read cannot answer.

An empty graph also reads zero, so a zero alone proves nothing. The reducer
records `eshu_dp_graph_id_anchor_id_bearing_nodes` from the same pass. The
healthy reading is `eshu_dp_graph_id_anchor_unreachable_nodes == 0` and
`eshu_dp_graph_id_anchor_id_bearing_nodes > 0`; both come from `/metrics`, with
no need to read the log line.

## How it is sampled

The census is one read-only `AllNodesScan` through the reducer's graph read
port. The reducer takes one pass at startup, then one per
`ESHU_ID_ANCHOR_CENSUS_POLL_INTERVAL` (default `1h`), each under an
`ESHU_ID_ANCHOR_CENSUS_TIMEOUT` deadline (default `2m`). It records the result
after the pass, so a scrape never reads the graph. The scan took about 1.95 s
over 1,130,424 nodes (898,874 with an id) on ops-qa, image sha-57167b0, on
2026-10-08. It runs on Neo4j only; set `ESHU_ID_ANCHOR_CENSUS_ENABLED=false` to
turn it off.

The value is a snapshot of one read transaction, not a point in time. A node
written during the scan may or may not be counted. A failed or timed-out pass
keeps the last good value of both gauges, so read the age beside the count.

## Signals

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_graph_id_anchor_unreachable_nodes` | gauge | Nodes with an id that the anchor cannot reach, from the last successful pass. No labels. |
| `eshu_dp_graph_id_anchor_id_bearing_nodes` | gauge | Nodes that carry an id, from the same successful pass. Zero beside a zero unreachable count is an empty graph, not a healthy one. No labels. |
| `eshu_dp_graph_id_anchor_census_last_success_unixtime` | gauge | Unix second of the last successful pass. `time()` minus this is the snapshot age. |
| `eshu_dp_graph_id_anchor_census_passes_total` | counter | Passes by `outcome`: `ok` or `failed`. |
| `eshu_dp_graph_id_anchor_census_duration_seconds` | histogram | Pass wall time by `outcome`. |

The pass log line is `id anchor census` with `snapshot=true`, `first_pass`,
`id_bearing_nodes`, `via_uid_nodes`, `via_id_nodes`, and `unreachable_nodes`. A
nonzero `unreachable_nodes` logs at WARN. A failed pass logs
`id anchor census pass failed` at ERROR with `failure_class=id_anchor_census_error`.

## Alerts

`deploy/observability/alerts.yaml` (group `eshu.pipeline`) and
`deploy/observability/id-anchor-census-prometheus-rule.yaml` (Prometheus
Operator) carry the same two alerts. The census alert rule test in
`go/internal/reducer/maintenance/census` keeps them aligned.

| Alert | Expression | For | Means |
| --- | --- | --- | --- |
| `EshuGraphIDAnchorUnreachableNodes` | `max(eshu_dp_graph_id_anchor_unreachable_nodes) > 0 and max(eshu_dp_graph_id_anchor_id_bearing_nodes) > 0` | 2h | Entity ids that entity context answers 404 on Neo4j. The 2h window spans two default passes, so one pass that sees a node mid-write does not page. |
| `EshuGraphIDAnchorCensusStale` | `(time() - max(eshu_dp_graph_id_anchor_census_last_success_unixtime) > 10800) or (sum(increase(eshu_dp_graph_id_anchor_census_passes_total{outcome="failed"}[3h])) > 0 unless sum(increase(eshu_dp_graph_id_anchor_census_passes_total{outcome="ok"}[3h])) > 0)` | 15m | The census has stopped succeeding: the last success is older than three default intervals (a reducer that once succeeded), or passes failed in the last 3h with none succeeding (a reducer that has not succeeded since it started, which never exports the last-success gauge). The unreachable count is then stale or absent, and the first alert cannot fire on new data. |

Neither alert covers a reducer that runs no census pass at all (the reducer
is down, or `ESHU_ID_ANCHOR_CENSUS_ENABLED=false`): it exports none of these
series. A reducer that is down is caught by the runtime health alerts, not by
these. Because `increase()` does not count the first sample of a new series,
the failed-pass clause becomes true from the second failed pass after a
restart. `unless` holds it back while any pass succeeded in the last 3h, so
the page timing depends on history:

- A fresh install with no earlier success pages about one poll interval after
  the restart (the second failed pass), then after the 15m `for`.
- A rollout of a previously healthy reducer pages about three intervals after
  the last success, when the predecessor's ok samples leave the 3h window,
  then after the 15m `for`. This matches the alert summary.

## When it is nonzero

A writer produced an id-bearing node on a label with no uid or id uniqueness
constraint, or a uid-constrained node whose uid is null or differs from its id.
On Neo4j, `GET /api/v0/entities/{entity_id}/context` and MCP
`get_entity_context` answer 404 for each of those ids until the writer is fixed
and the nodes are rewritten. The read has no unlabeled fallback since #7212. Do
not add one back.

1. Read the reducer log line `id anchor census` (WARN when nonzero). It gives
   `unreachable_nodes`, `via_uid_nodes`, `via_id_nodes`, and `id_bearing_nodes`.
   A count that rises between passes is a writer that is still running.
2. Find the label sets with `ResidualByLabelsCypher` (read-only; labels and
   counts, never ids).
3. Find the writer. Run the golden-corpus `writer-coverage` phase
   (`go/cmd/golden-corpus-gate`, `-phase=writer-coverage`) over a capture that
   drives it, or search the Cypher literals for an `id` write on that label.
4. Fix the writer: keep `id == uid` on a uid-constrained label, or add a uid or
   id uniqueness constraint for a new label (that widens the anchor; see
   `go/internal/query/entity/AGENTS.md`). Nodes written before the fix stay
   unreachable until the fixed writer rewrites or retracts them. Confirm that a
   later pass reads 0.
The golden-corpus writer-coverage gate and census check should have caught the
writer; if they did not, the writer runs on a path the corpus replay does not
drive. Find the label set with the census breakdown query in
`go/internal/graph/anchor` (`ResidualByLabelsCypher`), then the writer that
sets `id` on that label.
