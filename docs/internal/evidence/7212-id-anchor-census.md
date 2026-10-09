# Entity context: id-anchor census, writer-coverage gate, and census gauge (#7212)

## Scope

This note covers the first of two changes that let the Neo4j entity-context
read drop its unlabeled fallback statement. This change adds no behavior to the
query path: `EntityContextStatements` still returns two statements on Neo4j.
It adds the proof and the operator signal that the second change depends on.

- `go/internal/graph/anchor`: the definition of which id-bearing nodes the
  labeled anchor reaches, the census Cypher, and a heuristic pre-filter over
  Cypher text that looks for statements that write a node id (the package README
  names the shapes it reports beside the test functions, and its known blind
  spots).
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

All residual lines were zero: no id-bearing node with a different uid, no
Directory with an id, no id on a label outside the schema, and no id-bearing
node without a label. The 7,911 nodes without a uid sit on six id-constrained
labels. The scan was one read transaction of about 1.95 s under Neo4j's
read-committed isolation. It is not a point in time, it saw one deployment, and
a writer deployed later can break it. That is why the invariant needs the gates
and the gauge below, not this count alone. The graph has 1,130,424 nodes; the
2,243,861 figure in older notes is db hits.

## The two gates

**Authority.** The authority that no unanchored id-bearing node ships is the
census: the required `graph/anchor_census` check on the Neo4j legs after the
replay, and the reducer gauge on each deployment. Writer coverage and the static
sweep below are a heuristic pre-filter over Cypher text. They name a writer early;
they do not decide.

**Writer coverage** (`-phase=writer-coverage`, replay half). Over the
differential capture recordings, `CheckWriters` looks for a statement that writes
a node id (a `MERGE`/`CREATE` map key, `SET n.id = ...`, or a dynamic
`SET n += <map>`) on a node whose labels include no label from the union of the
uid-constrained and id-constrained label sets. A dynamic map on an uncovered
label is reported unless the recorded parameters show the map has no `id` key at
any depth. It reports the shapes its test rows cover. The package README names
them beside the test functions: `TestCheckWriters`,
`TestCheckWritersUnparsedShapes`, `TestCheckWritersPatternContextShapes`,
`TestCheckWritersRebindingAndProcedureShapes`, and
`TestCheckWritersScopeTargetAndRemovalShapes`, with the covered-label controls in
`TestCheckWritersStricterParserKeepsCoveredWritesClean`,
`TestCheckWritersPatternContextKeepsCoveredWritesClean`, and
`TestCheckWritersScopeTargetAndRemovalKeepProductionClean`. It is not a full
Cypher parser. The phase fails on an empty capture and on a capture that never
sees an id write. All four capture legs join, because the writers are the same
code on both backends.

Known blind spots (not exhaustive), each backed by the census check and gauge:
scope loss through a `CALL` subquery (an unlabeled re-declaration of an outer
name inherits the earlier label); procedure writers outside the
`apoc.create/merge/cypher/do/periodic/refactor` families (`db.create.setNodeVectorProperty`,
`apoc.atomic.add`); an UNWIND alias rebound by a form other than `AS name`, a
comprehension variable, or a FOREACH variable (a `YIELD` column of the same
name); and Cypher the scan does not model. `TestCheckWritersKnownBlindSpots`
holds a row for the ones with a concrete example.

**Static sweep** (`TestEveryProductionIDWriterNamesAnAnchorLabel`,
`TestEveryDynamicLabelWriterIsMarkedWithAProof`). A test over the non-test Go
files under `go/` that does not depend on what a replay executes. It admits a
string literal, or a `+` chain whose node pattern sits in a literal or a
same-package constant it can resolve (an operand it cannot resolve reads as a
placeholder). The text needs a write keyword, an `id` token or `+=`, and a node
pattern that is labeled (`(n:L` or `(n IS L`), unlabeled with an `id` key, or has
a placeholder label (also `%[1]s`); or a parameter property map; or a
`REMOVE n:L`. Measured on the final tree by the test's own walk: 93 admitted
sites, 80 with a static label and 13 with a placeholder label; of the 80, 76
report id writes and 4 report none; the analyzer reports nothing on them.
The 13 placeholder-label sites (the canonical and semantic entity upsert
templates, the `internal/graph` entity merge helpers, the read-API latency seed
tool) cannot be decided statically, so each carries a co-located marker comment
that names a test, defined in the marker's own directory, that proves its labels
are anchor labels. A marker pairs 1:1 with the nearest template below it; the
sweep reports a template with no marker of its own, a marker naming no test in
its directory, and a marker with no template of its own under it. The four proofs
are `TestEntityUpsertTemplateLabelsAreAnchorLabels` (the canonical entity labels
except Parameter, which the generic entity phase never writes, are anchor
labels), `TestSemanticEntityUpsertLabelsAreAnchorLabels` (the plan labels are
anchor labels, and so is every candidate type it offers `buildSemanticEntityRowMap`
that the function accepts: the plan labels, the canonical entity labels and four
probes; a type new to the codebase and added only to that filter is outside the
proof, and the census on the replay is its backstop),
`TestEntityMergeHelpersHaveNoProductionCaller` (no production file other than the
helpers' own calls them), and `TestSeedLabelsAreAnchorLabels`.

Known sweep blind spots (not exhaustive), each backed by the census check and
gauge: a `+` chain whose node pattern sits in an operand it cannot resolve (a
package `var`, a cross-package constant, a function call); `strings.Builder` or
other runtime assembly; a `fmt.Sprintf` verb that supplies the `id` key; and a
statement reached only through a bare constant name whose text is a fragment.
`TestSweepKnownBlindSpots` holds a row for each with a concrete example.

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
| Marker pairing | a second template under an existing marker; two markers over one template; a proof test in another directory | the sweep reports each (`TestOneMarkerExcusesOnlyTheNearestWriter`, `TestMarkerProofMustLiveInTheMarkersDirectory`) |
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
cross-check. The rollout of the second change is gated on `unreachable_nodes = 0
AND id_bearing_nodes > 0` from the same pass (both are in the `id anchor census`
log line, and both are gauges on `/metrics`: `eshu_dp_graph_id_anchor_unreachable_nodes`
and `eshu_dp_graph_id_anchor_id_bearing_nodes`), on each deployment, or on a read-only census of the same shape under
its own admission. A reading of zero with `id_bearing_nodes = 0` proves nothing:
an empty graph reads zero. The gauge detects after the fact, with a lag of up to
one poll interval (default one hour).

## Performance Evidence

Performance Evidence: the census adds one read-only `AllNodesScan` per pass per
reducer replica, off the request path and off the scrape path. The same shape
of scan took about 1.95 s over 1,130,424 nodes (898,874 with an id) on ops-qa,
image sha-57167b0, on 2026-10-08, in one read transaction. At the default hourly
interval that is under 0.1% of one core. The two-minute timeout is about 60
times the measured scan, so a graph that grows several-fold still finishes and a
wedged read cannot hold the loop. The scan does not write, takes no lock the
write path waits on under read-committed isolation, and needs no lease because
each replica reports its own snapshot (read at its own time; alert on the
maximum across replicas). The golden-corpus check runs the same statement once
per Neo4j leg on a corpus far smaller than ops-qa.

The statement was not re-profiled in this change: the figure above is the
measured pass of the same pattern, and the Cypher here adds only the per-row
label tests. The statement ran on a real Neo4j for the first time in this
change (see Observability Evidence). `NOT_CHECKED`: the plan and db hits of this
exact statement on a graph the size of ops-qa.

## Observability Evidence

Observability Evidence: five instruments, all in the reducer maintenance lane:
`eshu_dp_graph_id_anchor_unreachable_nodes`,
`eshu_dp_graph_id_anchor_id_bearing_nodes` (recorded from the same pass, so the
rollout read `unreachable = 0 AND id_bearing > 0` comes from `/metrics` alone),
`eshu_dp_graph_id_anchor_census_last_success_unixtime`,
`eshu_dp_graph_id_anchor_census_passes_total` (closed `outcome`: `ok`,
`failed`), and `eshu_dp_graph_id_anchor_census_duration_seconds`. None carries a
label that grows with data. The tests in
`go/internal/reducer/maintenance/census/runner_test.go` drive the
runner against a real OpenTelemetry SDK manual reader and assert the gauge value,
the id-bearing gauge (the pass's count, a recorded zero on an empty graph, and the
last good value after a failed pass), the last-success time, the pass counter, the duration histogram, the startup log
line (`snapshot=true`, `first_pass=true`, the counts), the WARN on a residual,
and that a failed pass or a timeout keeps the last good snapshot.

The signal was also observed against a real Neo4j (`neo4j:2026-community`, the
digest pinned in `docker-compose.live-backend-neo4j.yml`, server 2026.08.1), on a
throwaway container on a free port, through the production wiring
(`startIDAnchorCensus` with the raw session runner and an OpenTelemetry SDK
reader). `TestLiveIDAnchorCensusStartup` ran the startup pass on an empty graph,
then after canonical shapes, then after one planted id-only node:

```text
startup pass, graph before seed: gauge=0 ok_passes=1
level=INFO msg="id anchor census" snapshot=true first_pass=true id_bearing_nodes=0 via_uid_nodes=0 via_id_nodes=0 unreachable_nodes=0 duration_seconds=0.0046
startup pass, one id-only node planted: gauge=1
level=WARN msg="id anchor census" snapshot=true first_pass=true id_bearing_nodes=3 via_uid_nodes=1 via_id_nodes=1 unreachable_nodes=1 duration_seconds=0.0023
```

`TestLiveAnchorCensus` agreed with the reference classification on each seeded
shape and failed when the `coalesce` was removed (the null-uid node vanished from
the count, residual moved by 1 instead of 2). `TestLiveAnchorCensusCheck` ran the
gate's own Bolt reader with the production schema applied: canonical shapes
passed (`id-bearing 2, via uid 1, via id 1, residual 0`), and two planted
unreachable nodes failed with `residual 2` and the label sets
`labels=[Function] nodes=1; labels=[Unconstrained] nodes=1` and no id. The
transcripts were captured locally.

All three live tests were re-run after the residual-by-labels statement moved its
two reachability booleans into a `WITH` (the NornicDB label-predicate guard
rejects a quantifier over `labels()` inside a `WHERE`, #6786 X11). The run was at
the commit titled "test(reducer): wait for the census gauges before the live test reads them" (the last commit with code or Cypher changes; its SHA changes on every rebase), rebased onto main at 35d869c4a, on the pinned
`neo4j:2026-community` image (server 2026.08.1, arm64 container, private host
ports; the final run kept its data in tmpfs because the Docker VM disk was full,
and used the same image digest as the compose service), torn down after. The same three tests also passed one rebase earlier,
before the runner moved to its leaf package. Later commits change only
documentation. `TestLiveAnchorCensus`
passed. `TestLiveAnchorCensusCheck` passed with the same clean line
(`id-bearing 2, via uid 1, via id 1, residual 0`) and the same planted result
(`residual 2`, label sets `labels=[Unconstrained] nodes=1; labels=[Function]
nodes=1`), which comes from the rewritten statement. `TestLiveIDAnchorCensusStartup`
passed with the id-bearing gauge read beside the residual (`gauge=0 id_bearing=0`
on the empty graph, then `gauge=1 id_bearing=3` with one id-only node planted).
The `coalesce` mutation was not repeated. `NOT_CHECKED`: a running reducer process
scraped over `/metrics`, and whether the ops-qa and ops-prod scrape
configurations collect the gauge.

## What only CI exercises

- The census check against a real replay's graph (the graph phase of
  `golden-corpus-gate-neo4j`, and the Neo4j legs of the differential job). The
  three live tests are rows in `specs/live-tests.v1.yaml` (class `ci`, Neo4j only,
  self-skipping elsewhere), so the live-backend CI job also runs them. They ran
  last on the commit titled "test(reducer): wait for the census gauges before the live test
  reads them" (the rebased head with the rewritten residual statement; see
  Observability Evidence above); later commits change only documentation.
- The writer-coverage phase over a real replay's recordings. The analyzer is
  proved on recorded-session fixtures and the sweep on the source tree, not on a
  full replay.
- Whether a clean replay leaves a residual of zero. The census check is
  required; a residual in the corpus fails the leg and is a finding to root-cause.

## What the census does not close

The census is the authority, and four classes stay outside what it can see:

1. **A writer the golden corpus never drives.** A code path with no fixture in
   the corpus (a collector or projection lane without a cassette, a repair,
   backfill or migration tool, a deployment-only path, the offline tier) produces
   no node in the gate graph, so the census check and writer coverage see nothing.
   The static sweep reads the Go source (a heuristic; its blind spots are listed
   above), the marker proofs cover the dynamic-label templates, and after
   deployment the gauge covers what has run there. Detection is after the fact,
   with a lag of up to one poll interval (default one hour).
2. **Legacy data on a deployment** written by older code. CI never sees it; the
   gauge does, on its startup pass. This is what the rollout condition above is
   for.
3. **A statement the replay records that produces no unreachable node** (an empty
   `UNWIND $rows`, a `MERGE` that matched an existing node, an `ON CREATE` that did
   not fire). Only the pre-filter's shape check sees it, and in a blind-spot shape
   it misses.
4. **A node written between two gauge passes.** Until the next pass warns and the
   writer is fixed, such a node is not found by the labeled anchor. This matters
   once the second change removes the fallback, not for this change, which alters
   no query behavior.

The hourly gauge on ops-qa and ops-prod covers class 1 for writers that have
already run there and class 2 wholesale. It does not pre-empt a future writer
(class 1) and does not close class 4. An alert rule on the gauge is a follow-up for
the second change.

Arbiter ruling 2026-10-08, repeat findings F3/F4: the pre-filter and the sweep are
heuristic, the census is the authority, and a further analyzer miss blocks only if
a non-test Go literal or a replay recording carries that shape.
