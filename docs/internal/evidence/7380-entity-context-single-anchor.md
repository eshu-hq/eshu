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
community (`neo4j:2026-community@sha256:eabfbb04...`, native arm64), schema
applied, 295-node fixture plus 300k filler nodes for db hits. Before/after,
measured by the #7380 shim on a shared host at load1 16-97 (every timing below
is NON-PD, i.e. not a quiet-host figure): restart-cold miss, 16 statements
3922/4403/9907 ms (min/median/max, n=5) against 2 statements 1485/1981/2257 ms
(n=5); found on label 15, 5285/7385/10064 ms against 2549/3236/3853 ms (n=3);
scoped miss 5790/6679 ms against 2736/2217/1967 ms; warm 63-750 ms against
23-110 ms. Explain-only planning was about 85% of the loop's cold cost. The
anchor plan is `NodeUniqueIndexSeek` per label (1 db hit each), 14 db hits on a
miss at 300k nodes, no scan; the unlabeled fallback is unchanged and still costs
about 600k db hits at 300k nodes on a miss or an id-only hit. The 2.2x cold
ratio is not a quiet-host claim; the quiet-host figure and native amd64 are
NOT_CHECKED.

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
