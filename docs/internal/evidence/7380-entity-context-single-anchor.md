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
community (`neo4j:2026-community@sha256:eabfbb04...`, native arm64), real schema
applied (261 statements), 295-node fixture, unscoped and scoped shapes. Before
is the unchanged 16-statement loop (byte-identical to the base commit's text,
checked by dumping both), after is the shipped statement text (the `CALL () {
UNION }` anchor with the `WITH ... anchor_rank ORDER BY ... LIMIT 1` wrapper,
then the unlabeled fallback), both taken from the production
`(*Handler).entityContextStatements`, not hand-copied. Every request is a
restart-cold round (fresh JVM, empty plan cache), n=5 per candidate per scenario,
loop and anchor interleaved with alternating first-mover. ALL FIGURES ARE NON-PD:
the shared host ran at load1 14-91 (median 41, 18 CPUs; rule PD needs load1 < 9
at start, end and in-run), 0 of 52 rounds met PD, and a back-to-back A/A control
pair differed by 39% (8582 ms vs 11891 ms), so run-to-run spread is large and the
absolute seconds are not a quiet-host claim; only the paired ratios are used.
Cold wall ms (min/median/max), loop against anchor: unscoped miss (16 against 2
statements) 5259/5865/10356 vs 1747/1953/2299, paired ratio median 2.98x (range
2.69-5.93x); scoped miss 5252/9391/13119 vs 2107/2806/8439, ratio median 2.78x
(range 1.11-4.16x, one anchor round hit load1 90); a hit on the 15th label
(WorkloadInstance, 15 against 1 statement) 5851/6269/9246 vs 1742/1879/6302, ratio
median 3.25x (range 0.93-5.07x). A hit on the FIRST label (Function, 1 against 1
statement) is slower with the anchor: 1412/1505/1823 vs 1829/1999/2949, anchor
slower in 5 of 5 paired rounds (ratio median 0.75x, range 0.48-0.98x), because the
anchor must plan its whole union cold while the loop plans only its first
statement. Cold EXPLAIN-only planning of every statement one request
sends: 3842/7779/9315 ms (loop) vs 2023/4527/6741 ms (anchor); planning is about
90% of each candidate's cold cost (median EXPLAIN against the run that follows it).
Warm (plans cached, 3 reruns per round): miss 113/215/419 vs 22/33/52 ms, 15th
label 120/167/468 vs 22/26/42 ms, first label 21/25/38 vs 15/28/79 ms (equal).
The shipped plan (PROFILE) is one `NodeUniqueIndexSeek` per label under a `Top`
for the rank wrapper: 14 db hits on a miss; the unlabeled fallback is unchanged
and still an `AllNodesScan` (591 db hits at 295 nodes, about 600k at 300k nodes
from the earlier theory shim). The earlier theory-shim timings (candidate D3, the
same union without the rank wrapper and `LIMIT 1`, host load1 16-97) are not the
shipped statement and are not reported here. NOT_CHECKED: a quiet-host (PD)
rerun, native amd64, a 300k-node cold rerun of the shipped text (the disk of the
measuring host filled and its Docker VM crashed after the main rounds), a truly
cold page cache, and the concurrent cold first-request stampede. Net effect
measured: fewer distinct texts to plan cold (16 to 2 per shape) is about 3x on a
miss or a late-label hit and a cold-request cost on a first-label hit; warm cost
is lower everywhere or equal.

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
truly cold page cache, native amd64, and the fallback scan itself (a candidate
follow-up is an id index on the id-only labels or a bounded fallback).
