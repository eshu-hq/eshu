# #7380: Neo4j entity context resolves its anchor in one statement

## Problem

`GET /api/v0/entities/{id}/context` (MCP `get_entity_context`) looped over up to
16 distinct Cypher statements, one per `EntityContextAnchorLabels` entry plus the
unlabeled fallback (`go/internal/query/entity/context_handler.go`), under one
shared 10 s bounded read. A cold Neo4j plans every distinct text it sees, and
the scoped caller shape has its own 16 texts, so 32 texts across both shapes.
A miss (an unknown id, or a label outside the list) paid the whole loop.
Diagnosed under #7353: not index population, not literals (the id is a bound
parameter), not execution; the plan is a one-db-hit uid seek.

## Fix

On Neo4j (`Handler.GraphBackend == querycontract.GraphBackendNeo4j`, wired in
`cmd/api` and `cmd/mcp-server`) the loop is two statements:

1. one `CALL () { <uid labels {uid: $id}> WHERE e.id = $id UNION <id labels {id: $id}> }`
   anchor, then `WITH e, <rank> AS anchor_rank ORDER BY anchor_rank LIMIT 1`,
   followed by the unchanged file/repo tail (`context_anchor_neo4j.go`);
2. the unchanged unlabeled fallback, only when the anchor returns no row.

That is at most 2 distinct statement texts per caller shape (4 in total) instead
of 16 (32). The form and Neo4j 5.23 floor are the ones
`codemodel.Neo4jEntityIDAnchor` already uses (#7057); the inline `{uid: $id}` map
is what makes Neo4j seek (a plain label disjunction plans as
`UnionNodeByLabelsScan`, see `7057-relationship-uid-anchor.md`).

- The uid labels are `EntityContextAnchorLabels` entries with a schema uid
  uniqueness constraint (`graph.HasUIDUniquenessConstraint`); the id labels are
  the remaining entries with a schema id uniqueness constraint (the new
  `graph.HasIDUniquenessConstraint`, parsed from the constraint DDL). Neither is
  a hand-kept list; a unit test recomputes both from the DDL and compares.
- `Directory` is dropped from the anchor. The canonical writer never sets
  `Directory.id` (`storage/cypher/canonical_node_cypher.go`, MERGE on path), and
  it has no id index, so its branch was a `NodeByLabelScan` over every Directory
  on every request that reached it. A Directory that does carry an id still
  resolves through the unlabeled fallback.
- Precedence: the loop stopped at the first label, in `EntityContextAnchorLabels`
  order, with a row. A union has no order, so each candidate is ranked by the
  position of its first label in that order and `LIMIT 1` keeps the best. An id
  shared by two labels resolves to the label the loop tried first, regardless of
  creation or plan order. `LIMIT 1` also bounds the tail to one anchor node.
- NornicDB and the zero value keep the loop unchanged: a label disjunction
  silently returns zero rows there and a many-branch `CALL {UNION}` did not
  return within 15 s (#7006). The loop statement text is byte-identical to
  before.

Performance Evidence: shape `GetEntityContext` anchor, Neo4j 2026.08.1
community (`neo4j:2026-community@sha256:eabfbb04...`, `linux/amd64`), real schema
applied, 295-node fixture, unscoped shape. Before is the unchanged 16-statement
loop (the zero-value `Handler`), after is the shipped statement text (the
`CALL () { UNION }` anchor with the `WITH ... anchor_rank ORDER BY ... LIMIT 1`
wrapper, then the unlabeled fallback). Both were taken from the production
`(*Handler).entityContextStatements`, not hand-copied; a dump of the loop and
anchor texts, unscoped and scoped, is byte-identical (`cmp` rc=0) to the dump
taken from the base commit.

Machine: a quiet 16-CPU x86_64 host (AMD EPYC 9R14, 1 thread per core, 132 GB
RAM, Ubuntu 24.04, Linux 6.17, Docker 29.3.1, Go 1.26.6) with no other gate or
build in the window (one idle Postgres container was up). The PD limit is load1 below 8.0 (`nproc`/2). load1 was sampled
every 1 s; a round counts only if load1 at start, at end and in-run were all
below 8.0. Across the 141 main-run rounds (70 calibration, 71 paired) the in-run
maximum was 2.70 and no round reached 8.0. For the 70 valid pairs load1 ran
0.85-2.28 at start, 0.85-2.28 at end and 1.19-2.70 in-run. At the start of the
run the host load average was 2.75 (0.03 before the build) and at the end 0.59.
Every pair also runs the loop, the unchanged base, as a control canary. Each
regime and scenario cell was calibrated with 2 sets of 5 loop-only rounds (10 of
10 valid, none over the load limit), and a pair whose loop run exceeded that
cell's mean + 3 SD (101.2 ms for W hit-first up to 3627.6 ms for R miss) is
invalid on both sides and re-run. One pair was invalid: W hit-late attempt 8,
load1 fine, loop request 1195.5 ms against a bound of 1190.2 ms; it was
discarded and re-run (attempt 11 valid). 70 valid pairs of 71 attempted, none
invalid on load. Wall times are reported, not gated: the gates are the
deterministic ones below.

Regimes, each request timed from the client: W is a warm JVM with
`db.clearQueryCaches()` before each request, so the plan cache is empty and
nothing else is cold. P restarts the container, runs a neutral 21-text prewarm
(none contains the entity id parameter, an id or uid anchor, or the anchor
union, so neither candidate's statements are warmed), then sends the request.
R restarts the container and the request is the first query ever. Warm is three
reruns after each R round, plans cached. Loop and anchor alternate the first
mover, and the cells within a regime are interleaved. Scenarios are a hit on
the first label (Function), the second label (Class), the 15th label
(WorkloadInstance) and a miss (an unknown id, all 16 loop statements). Cold
request wall ms, min/median/max over 10 valid pairs per cell (5 for the last
row), loop against anchor; S-A is the paired anchor minus loop, mean (SD):

| regime, scenario | loop | anchor | S-A ms | anchor slower |
|---|---|---|---|---|
| W, hit first (1 vs 1 stmt) | 79.2/80.5/85.2 | 117.1/118.7/151.0 | +41.20 (8.67) | 10 of 10 |
| W, hit second (2 vs 1) | 153.8/156.1/160.7 | 116.4/119.4/122.9 | -37.01 (1.01) | 0 of 10 |
| W, hit 15th (15 vs 1) | 1125.3/1135.8/1168.0 | 115.6/118.2/122.8 | -1022.04 (11.41) | 0 of 10 |
| W, miss (16 vs 2) | 1203.1/1209.1/1239.9 | 189.2/190.1/198.9 | -1021.51 (8.79) | 0 of 10 |
| P, hit first | 355.1/360.7/391.2 | 626.8/642.9/665.2 | +276.72 (20.87) | 10 of 10 |
| R, hit first | 906.4/909.1/952.1 | 1200.1/1208.4/1229.8 | +296.24 (17.88) | 10 of 10 |
| R, miss (16 vs 2) | 3134.8/3169.3/3330.1 | 1405.8/1415.4/1439.6 | -1768.59 (61.37) | 0 of 10 |
| warm, hit first (30 reruns) | 10.4/12.1/22.0 | 11.2/13.6/18.7 | +1.46 (1.10) | 9 of 10 rounds |
| warm, miss (30 reruns) | 46.2/50.0/63.4 | 14.2/17.7/25.3 | -34.12 (0.76) | 0 of 10 |
| R, miss at 300295 nodes (5 pairs) | 3269.1/3329.1/3456.8 | 1536.2/1550.1/1575.2 | -1785.82 (81.95) | 0 of 5 |

The 300k row used the same driver, host and image with 100000 extra Function,
Class and Variable nodes each, its own calibration (bound 3616.2 ms) and a
warm rerun of 99.0/109.8/134.1 against 63.1/81.4/91.0 ms (S-A -35.40, SD 5.95).
The loop over anchor ratio median is 6.35 (W miss), 9.62 (W hit 15th), 2.24
(R miss) and 2.15 (R miss at 300k), so the saving is far outside the run-to-run
spread; it has a deterministic cause, 16 statement texts and round trips
becoming 2. The slowest valid anchor request was 1439.6 ms (1575.2 ms at 300k),
inside the 10 s shared bounded read.
`result_first_row` was equal in every pair (70 valid and the 1 invalid), in the
5 pairs at 300k and in every warm rerun; hit scenarios returned a row and miss
scenarios none.

First-label cost, measured. A request whose id resolves on the first label
(Function) used to plan one statement and now plans the whole anchor. When the
anchor's plan is not cached that request is slower: +41.2 ms with a warm JVM
and a cleared plan cache (W, n=10, SD 8.7, anchor over loop 1.51x, SD 0.09),
+276.7 ms after a restart and a neutral prewarm (P, n=10, SD 20.9, 1.76x) and
+296.2 ms as the first query after a restart (R, n=10, SD 17.9, 1.32x). With
the plan cached the two are equal within a millisecond and a half (12.1 against
13.6 ms median, +1.46 ms paired mean, SD 1.10). Neo4j keeps a plan until its
statistics diverge (`dbms.cypher.statistics_divergence_threshold`, checked at
most every `dbms.cypher.min_replan_interval`), so the cost is per plan epoch,
not per request: at most one request per caller shape (unscoped, scoped) pays
it after a Neo4j restart or a replan. Only label position 1 loses. The loop
adds about 75 ms per further label in W (156.1 ms at position 2, 1135.8 ms at
position 15, against 80.5 ms at position 1), the anchor is flat at about 118
ms, so a hit on the second label is already 37 ms faster and one non-Function
request or one miss in the epoch repays the first-label penalty. No first-label
fast path was added: it would add a third statement text and a round trip to
every other request.

Where the cause was observed. In W, with the JVM warm and only the plan cache
cleared, the extra cost is present (+41 ms) and disappears once plans are
cached (+1.5 ms), which points at planning the larger union. The P and R
penalties are about 7 times larger and were observed after a container restart;
the split between planning and first execution of the larger plan there is
unproven. NOT_CHECKED: that split, whether `EXPLAIN` fills the executable-plan
cache, the production share of Function ids among entity-context requests,
timing of the scoped shape (only its statement text was verified byte-identical),
a P-regime miss cell, a hit-first cell at 300k nodes, a truly cold page cache
(the host held 112 GB of page cache) and the concurrent cold first-request
stampede.

Deterministic gates, unchanged: at most 2 statements on a full miss, one
`NodeUniqueIndexSeek` per label and no scan in the shipped plan. The shipped
plan (PROFILE) is one `NodeUniqueIndexSeek` per label under a `Top` for the
rank wrapper: 14 db hits on a miss; the unlabeled fallback is unchanged and
still an `AllNodesScan` (591 db hits at 295 nodes, about 600k at 300k nodes
from the earlier theory shim). An earlier run of this branch on a loaded shared
host (load1 14-91) is not reported: no round met PD and its spread was too
large to read.

Observability Evidence: the read keeps the `entity.context` graph query name,
the shared `WithBoundedGraphReadDeadline`, the `neo4j.query` spans,
`eshu_dp_neo4j_query_duration_seconds`, the `query.graph_read.warning` log and the
handler's anchor-loop failure log. `labels_total` in that log now equals the
number of statements the handler would send (2 on Neo4j, 16 on NornicDB), and
`labels_tried` the number sent. No metric, span, route or response shape
changes.

## Proof

- Unit: `context_anchor_neo4j_test.go` asserts, through a recording reader, at
  most 2 statements on a full miss for the unscoped and scoped shapes, no
  fallback after an anchor hit, fallback only on an anchor miss, constant text
  across ids, the label sets against the DDL, the rank order against the loop's
  try order, and that NornicDB and the zero value still send the 16-statement
  loop. `cmd/api` and `cmd/mcp-server` wiring tests fail if the backend is not
  passed to the entity handler.
- Live Neo4j (`context_anchor_neo4j_live_test.go`): the seeded graph is read
  through the loop (NornicDB dialect, valid on Neo4j) and through the anchor; the
  responses must be identical for 14 scenarios: found at label positions 1, 2,
  10 and 15, id-keyed Repository and Workload, Directory with an id, an id-only
  Function, an off-list Trait, a File that has only a uid (must not match, two
  variants), a missing id, and two ids shared by two labels, one created in the
  reverse of rank order; plus a scoped in-grant and out-of-grant pair. It also
  asserts at most 2 statements and an `EXPLAIN` with `NodeUniqueIndexSeek` and no
  `NodeByLabelScan`, `AllNodesScan` or `UnionNodeByLabelsScan`, for both shapes.
  Reversing the rank order fails the two shared-id scenarios; dropping the
  `e.id` equality fails both File scenarios.

Not covered: production-distribution ids (shared ids across labels should not
occur, canonical uids are sha-derived), the cold first-request stampede, a
truly cold page cache, and the fallback scan itself (a candidate
follow-up is an id index on the id-only labels or a bounded fallback).
