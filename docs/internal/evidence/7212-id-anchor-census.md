# Entity context: id-anchor census, writer-coverage gate, and census gauge (#7212)

## Scope

This note covers the first of two changes that let the Neo4j entity-context
read drop its unlabeled fallback statement. This change adds no behavior to the
query path: `EntityContextStatements` still returns two statements on Neo4j.
It adds the proof and the operator signal that the second change depends on.

- `go/internal/graph/anchor`: the definition of which id-bearing nodes the
  labeled anchor reaches, a fail-closed analyzer for statements that write a
  node id, and the census Cypher.
- `golden-corpus-gate`: the opt-in `writer-coverage` phase over the capture
  recordings, and the required `graph/anchor_census` check on the Neo4j leg.
- The reducer's id-anchor census `Runner` (`internal/reducer/maintenance/census`): a bounded periodic census, a gauge, and
  a startup log line.

#7212 stays open. The post-deploy warm sweep decides the close.

## The measurement behind the design

A read-only census on ops-qa, image sha-57167b0, on 2026-10-08, counted the
graph by node, not by label:

| Count | Nodes |
| --- | --- |
| All nodes | 1,130,424 |
| Nodes with an `id` | 898,874 |
| Reached through a uid-constrained label with `uid` equal to `id` | 890,963 |
| Reached through an id-constrained label (no uid) | 7,911 |
| Reached through both | 0 |
| Not reachable by the labeled anchor (residual) | 0 |

Every residual line was zero: no id-bearing node with a different uid, no
Directory with an id, no id on a label outside the schema, and no id-bearing
node without a label. The 7,911 nodes without a uid sit on six id-constrained
labels. The scan was one read transaction of about 1.95 s under Neo4j's
read-committed isolation. It is not a point in time, it saw one deployment, and
a writer deployed later can break it. That is why the invariant needs the gates
and the gauge below, not this count alone. The graph has 1,130,424 nodes; the
2,243,861 figure in older notes is db hits.

## The two gates

**Writer coverage** (`-phase=writer-coverage`, replay half). Over the
differential capture recordings, every statement that writes a node id must name
at least one label in the union of the uid-constrained and id-constrained label
sets. A write is `MERGE`/`CREATE` with an `id` key in a node property map,
`SET n.id = ...`, or a dynamic `SET n += <map>`. The check is fail-closed: an
unlabeled variable is a finding, and a dynamic map on an uncovered label is a
finding unless the recorded parameters prove the map has no `id` key at any
depth. A relationship variable is not a node and is skipped. An empty capture,
and a capture that never sees an id write, both fail, because either would pass a
blind analyzer. All four capture legs join, because the writers are the same
code on both backends.

**Static sweep** (`TestEveryProductionIDWriterNamesAnAnchorLabel`). An
independent test over the Cypher string literals and constant concatenations in
`go/`. It does not depend on what a replay executes. A statement whose label is
built at run time is invisible to it; the replay half covers those.

**Census check** (`graph/anchor_census`, graph phase, Neo4j leg). After the
replay, the count of nodes with an id that are not anchor-reachable must be zero,
and the graph must hold at least one id-bearing node. The check is required and
corpus-size independent. Off Neo4j it records a non-required skip. A failure
prints the label sets of the unreachable nodes (at most 20) and no ids.

The census Cypher keeps `coalesce(n.uid = n.id, false)`. Without it, a node with
a null uid makes `NOT (null AND ...)` null, a `WHERE` drops it, and the
unreachable node disappears from the count. `TestLiveAnchorCensus` plants that
shape and an id-only node on an unconstrained label and requires the residual to
rise by two.

### Seeded violations

| Gate | Planted violation | Result |
| --- | --- | --- |
| Writer coverage, replay | `MERGE (n:Unconstrained {id: $entity_id})` in a recording | gate fails, report names the label and the builder |
| Writer coverage, dynamic | `SET n += row.props` on an uncovered label, `props` carries `id` | gate fails |
| Writer coverage, dynamic | same statement, no `id` key in any row | gate passes |
| Static sweep | the same `MERGE` in a Go file under the real tree | test fails, names file and line |
| Census check | one id-only node on an unconstrained label | check fails |
| Census check | one `Function` with an id and no uid | check fails |
| Census check | clean canonical nodes | check passes |

## Operator signal

`eshu_dp_graph_id_anchor_unreachable_nodes` is a snapshot gauge of the same
count, read by the reducer on Neo4j. The reducer takes one pass at startup, so
the startup log line `id anchor census` carries the count, then one pass per
`ESHU_ID_ANCHOR_CENSUS_POLL_INTERVAL` (default one hour) under an
`ESHU_ID_ANCHOR_CENSUS_TIMEOUT` deadline (default two minutes). The result is
recorded after the pass, never during a scrape. A failed or timed-out pass leaves
the gauge and `eshu_dp_graph_id_anchor_census_last_success_unixtime` at their
last good values, so the snapshot age grows. A nonzero count logs at WARN.

The gauge is the traffic-independent check that the invariant holds on a
deployment. The existing `resolved_by` counter stays as the traffic-dependent
cross-check. The rollout of the second change is gated on this gauge reading
zero on each deployment, or on a read-only census of the same shape under its own
admission.

## Performance Evidence

Performance Evidence: the census adds one read-only `AllNodesScan` per pass per
reducer replica, off every request path and off the scrape path. The same shape
of scan took about 1.95 s over 1,130,424 nodes (898,874 with an id) on ops-qa,
image sha-57167b0, on 2026-10-08, in one read transaction. At the default hourly
interval that is under 0.1% of one core. The two-minute timeout is about 60
times the measured scan, so a graph that grows several-fold still finishes and a
wedged read cannot hold the loop. The scan does not write, takes no lock the
write path waits on under read-committed isolation, and needs no lease because
each replica reports the same snapshot. The golden-corpus check runs the same
statement once per Neo4j leg on a corpus far smaller than ops-qa.

The statement was not re-profiled in this change: the figure above is the
measured pass of the same pattern, and the Cypher here adds only the per-row
label tests. `NOT_CHECKED`: the plan and db hits of this exact statement on
Neo4j; `TestLiveAnchorCensus` (tag `live_nornicdb_answer_truth`) and the
`golden-corpus-gate-neo4j` leg run it in CI.

## Observability Evidence

Observability Evidence: four instruments, all in the reducer maintenance lane:
`eshu_dp_graph_id_anchor_unreachable_nodes`,
`eshu_dp_graph_id_anchor_census_last_success_unixtime`,
`eshu_dp_graph_id_anchor_census_passes_total` (closed `outcome`: `ok`,
`failed`), and `eshu_dp_graph_id_anchor_census_duration_seconds`. None carries a
label that grows with data. The tests in
`go/internal/reducer/maintenance/census/runner_test.go` drive the
runner against a real OpenTelemetry SDK manual reader and assert the gauge value,
the last-success time, the pass counter, the duration histogram, the startup log
line (`snapshot=true`, `first_pass=true`, the counts), the WARN on a residual,
and that a failed pass or a timeout keeps the last good snapshot. The signal was
not observed in a local reducer run: that needs a Neo4j container, which this
change did not start. `NOT_CHECKED`: the gauge on a live reducer, and whether the
ops-qa and ops-prod scrape configurations collect it.

## What only CI exercises

- The census Cypher against a real Neo4j (`TestLiveAnchorCensus`, and the graph
  phase of `golden-corpus-gate-neo4j`).
- The writer-coverage phase over a real replay's recordings. The analyzer is
  proved on recorded-session fixtures and on every Cypher literal in `go/`, not
  on a full replay.
- Whether a clean replay leaves a residual of zero. The census check is
  required; a residual in the corpus fails the leg and is a finding to root-cause.
