# Id-Anchor Census

On Neo4j the reducer publishes `eshu_dp_graph_id_anchor_unreachable_nodes`, a
snapshot of graph nodes that carry an `id` the labeled entity-context anchor
cannot reach. The anchor reaches a node through an id-constrained label, or
through a uid-constrained label with `uid` equal to `id`. Zero is the
invariant: with it, a miss on `GET /api/v0/entities/{entity_id}/context` has no
node left for an unlabeled scan to find.

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
keeps the last good value, so read the age beside the count.

## Signals

| Metric | Type | Use |
| --- | --- | --- |
| `eshu_dp_graph_id_anchor_unreachable_nodes` | gauge | Nodes with an id that the anchor cannot reach, from the last successful pass. No labels. |
| `eshu_dp_graph_id_anchor_census_last_success_unixtime` | gauge | Unix second of the last successful pass. `time()` minus this is the snapshot age. |
| `eshu_dp_graph_id_anchor_census_passes_total` | counter | Passes by `outcome`: `ok` or `failed`. |
| `eshu_dp_graph_id_anchor_census_duration_seconds` | histogram | Pass wall time by `outcome`. |

The pass log line is `id anchor census` with `snapshot=true`, `first_pass`,
`id_bearing_nodes`, `via_uid_nodes`, `via_id_nodes`, and `unreachable_nodes`. A
nonzero `unreachable_nodes` logs at WARN. A failed pass logs
`id anchor census pass failed` at ERROR with `failure_class=id_anchor_census_error`.

## When it is nonzero

A writer produced an id-bearing node on a label with no uid or id uniqueness
constraint, or a uid-constrained node whose uid is null or differs from its id.
The golden-corpus writer-coverage gate and census check should have caught the
writer; if they did not, the writer runs on a path the corpus replay does not
drive. Find the label set with the census breakdown query in
`go/internal/graph/anchor` (`ResidualByLabelsCypher`), then the writer that
sets `id` on that label.
