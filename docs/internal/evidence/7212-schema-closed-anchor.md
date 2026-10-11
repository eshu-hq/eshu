# Entity context: schema-closed Neo4j anchor, unlabeled fallback removed (#7212 PR-B)

## Scope

On Neo4j, `GET /api/v0/entities/{entity_id}/context` (and MCP
`get_entity_context`, which calls that route through
`go/internal/mcp/entity/resolution/routes.go`) now sends one graph statement:
the indexed `CALL () { ... UNION ... }` anchor over every uid- and
id-constrained label. The unlabeled `MATCH (e) WHERE e.id = $entity_id`
statement that followed it on a miss is gone. The NornicDB per-label loop, and
the unlabeled fallback that ends it, do not change. The design is the
"schema-closed anchor" decision on #7212 (2026-10-08). PR-A (#7824) added the
census that this change depends on.

## The two issue items

1. **Directory read.** This item was already done on Neo4j before this change.
   #7412 replaced the per-label loop with one `CALL ()` anchor over
   schema-constrained labels, and #7704 widened that anchor to all 124 of them.
   `Directory` has no uid or id constraint, so it was never in the anchor
   (`TestNeo4jEntityContextAnchorLabelSetsAreTheSchemaConstraints`). Only the
   NornicDB per-label loop still reads `Directory` at position 8. The design
   decision keeps that loop unchanged.
2. **Unlabeled fallback.** This change removes it from Neo4j.

## Why no answer is lost

**Directory never has an id.** The canonical writer
(`canonicalNodeDirectoryNodeCypher` in
`go/internal/storage/cypher/canonical_node_cypher.go`) runs
`MERGE (d:Directory {path: row.path})`. It sets `name`, `repo_id`, `scope_id`,
`generation_id` and `evidence_source`, and never sets `id`. The
backend-conformance corpus writer also keys `Directory` on `path`. The writer
sweep `TestEveryProductionIDWriterNamesAnAnchorLabel` fails on any production
statement that writes an id on a label outside the anchor set, and `Directory`
is outside that set. It passes on this tree.

**No caller receives a Directory id.** `resolve_entity` returns `e.id`
(`go/internal/query/entity/handler.go`). For a `Directory` that value is null,
so the row has no id, and `isAnonymousContainer` treats it as an anonymous
container (`resolve_results.go`). A caller therefore cannot get a Directory id
from Eshu to pass to entity context.

**Every writer-produced id-bearing node is reachable.** The census definition
(`anchor.Classify`, `anchor.CensusCypher`) counts each id-bearing node that sits
on no id-constrained label and on no uid-constrained label with `uid = id`.
Those are the nodes that only the unlabeled fallback could find. Readings:

| Where | Pass (UTC) | `unreachable_nodes` | `id_bearing_nodes` | via uid | via id |
| --- | --- | --- | --- | --- | --- |
| The QA environment, reducer image `1ac63e871a` | 2026-10-11 16:58:08 | 0 | 901,221 | 893,226 | 7,995 |
| The QA environment, same image | 2026-10-11 17:58:11 | 0 | 901,302 | 893,226 | 8,076 |
| Golden-corpus replay (CI, PR-A head) | Neo4j legs | 0 | 953 | 881 | 72 |

The rollout gate from PR-A is `unreachable_nodes = 0 AND id_bearing_nodes > 0`
from one pass. The QA environment meets it on both passes. **The production
environment has not been read: this change must not merge until that read
meets the gate.**

## What the tests prove

| Test | Proves | RED evidence |
| --- | --- | --- |
| `TestNeo4jEntityContextSendsOnlyTheIndexedAnchor` | a Neo4j miss sends one statement, the `CALL ()` anchor, for both caller shapes | failed before the change: 2 reads on a full miss |
| `TestNeo4jEntityContextMissKeepsTheNotFoundShape` | a Neo4j miss answers the same 404 body and headers as the NornicDB loop, `resolved_by=none`, one statement tried | failed before the change: `statements_tried` 2 |
| `TestNeo4jEntityContextAnchorAgreesWithTheCensusOnEveryLabel` | for each of the 124 census labels, in three shapes (canonical, id-only, split uid), and for a Directory with an id: the anchor answers in one statement exactly when `anchor.Classify` says the node is reachable | failed before the change: 235 unreachable shapes answered 200 through the fallback; seeded mutation (TerraformVariable left out of the anchor) fails with 123 of 124 canonical labels |
| `TestGetEntityContextResolvedByAnchorOnNeo4jFastPath` (existing) | a one-statement Neo4j hit reports `resolved_by=anchor` | seeded mutation (old `graphResolvedBy`) reports `fallback` |
| `TestLiveNeo4jSchemaClosedAnchorAgreesWithTheCensus` (live, Neo4j) | on a real Neo4j, one seeded node per census label resolves in one statement with the loop oracle's body; the three unreachable shapes resolve through the loop's fallback and answer 404 on Neo4j | seeded mutation (fallback restored) fails on the three shapes; seeded mutation (TerraformVariable left out) fails on that label |
| `TestLiveNeo4jEntityContextAnchorMatchesLoop`, `TestLiveEntityContextUIDAnchor` (live, updated) | the earlier scenarios still match the loop. The id-only Function, Directory with an id, and split-uid TerraformVariable are census-unreachable and answer 404 on Neo4j | rows changed with the contract |
| `TestAllowlistCoversCurrentEntityContextReads` | the fallback text is sent 0 times on Neo4j and once on NornicDB, and every statement is excused | failed before the allowlist entry |

## Performance and observability summary

Performance Evidence: baseline is origin/main 3893445a26 (anchor, then the
unlabeled fallback on a miss); after is this change (anchor only). Backend:
Neo4j 2026.08.1 community on the QA environment, read-only PROFILE, 3
interleaved rounds with alternating first mover. A miss fell from 2,267,161 db
hits (124 + 2,267,037) and 1,086 to 1,214 ms server time to 124 db hits and 2
to 4 ms warm, with 0 rows both ways. A hit sends the same single statement
before and after (309 to 10,224 db hits by label, 1 row with the expected label,
9 populated labels). On a local Neo4j 2026.05.0 seeded with one node per census
label, all 124 labels returned the same row before and after at 307 to 308 db
hits. The input shape is one entity id per request; there is no queue or
worker. It is safe because the QA reducer census read 0 unreachable nodes of
901,221 and 901,302 id-bearing nodes on two passes, so no id reachable only
through the removed statement existed there. Tables follow.

Observability Evidence: no new signal. The existing
eshu_dp_entity_context_resolution_total{resolved_by} counter and the
eshu.entity_context.resolved_by and eshu.entity_context.statements_tried span
attributes show the change: on Neo4j a miss reads resolved_by=none with
statements_tried=1 where it read 2, and fallback no longer appears. The census
gauge eshu_dp_graph_id_anchor_unreachable_nodes counts the ids that would now
answer 404 (see Telemetry below).

## PROFILE: the QA environment

Neo4j 2026.08.1 community. Read-only `PROFILE` through `cypher-shell
--access-mode read`, one statement at a time, 2026-10-11. In total: 19
`PROFILE` statements and one read to choose sample ids. No statement ran longer
than 1.3 s. Server time is the sum of the two `cypher-shell` timing figures.
The before path on a miss is the anchor and then the fallback. On a hit, the
before path and the after path send the same single anchor statement, so one
`PROFILE` per label measures both.

Misses (a new absent id per round, alternating first mover):

| Round | Order | Before: db hits (anchor + fallback) | Before: server ms | After: db hits | After: server ms | Rows before / after |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | after first | 124 + 2,267,037 = 2,267,161 | 4 + 1,210 | 124 | 291 (first call; plan compile), 2 on the repeat | 0 / 0 |
| 2 | before first | 124 + 2,267,037 = 2,267,161 | 5 + 1,081 | 124 | 2 | 0 / 0 |
| 3 | after first | 124 + 2,267,037 = 2,267,161 | 2 + 1,081 | 124 | 4 | 0 / 0 |

Hits (one canonical id per populated label; the row carried the expected label
in every case):

| Label | db hits | server ms | rows |
| --- | --- | --- | --- |
| Function | 365 | 3 | 1 |
| Class | 406 | 2 | 1 |
| TerraformVariable | 341 | 2 | 1 |
| TerraformResource | 330 | 33 | 1 |
| K8sResource | 330 | 1 | 1 |
| Repository | 10,224 | 7 | 1 |
| Workload | 309 | 1 | 1 |
| WorkloadInstance | 495 | 1 | 1 |
| Endpoint | 313 | 1 | 1 |

`CloudAction` had no node on the QA environment.

## PROFILE: every census label, local Neo4j

Local Neo4j 2026.05.0 community. This was the image already on the host,
not the repo's pinned digest; the pull was not available. The seed was the
live test's graph: one node per census label plus the three unreachable
shapes.

- Hits, 124 labels: before and after sent the same anchor statement (the
  before path stopped at the anchor row). 117 labels at 308 db hits and 7 at
  307. Each returned 1 row with the seeded label.
- Misses, 3 rounds, alternating first mover: after 124 db hits and 0 rows;
  before 379 db hits (anchor 124 + fallback 255 on this small graph) and 0 rows.
- The three census-unreachable shapes: after 124 db hits and 0 rows (404).
  Before, the fallback returned 1 row (436 db hits). This is the behavior
  change, and the census gauge measures it on a deployment.

## Telemetry

There is no new signal. `eshu_dp_entity_context_resolution_total{resolved_by}`
keeps its four values. On Neo4j it now records `anchor`, `content` or `none`,
never `fallback`, and `eshu.entity_context.statements_tried` is 0 or 1. An
operator finds lost answers on the census gauge
`eshu_dp_graph_id_anchor_unreachable_nodes`: on Neo4j each counted node is an
entity id that entity context answers 404. A 404 increase with
`resolved_by=none` and a nonzero gauge point to a writer that left the schema
contract.

## Gates moved with the change

- `specs/backend-divergence-allowlist.v1.yaml`: a `missing`-tier entry for the
  fallback text, which only the NornicDB leg records now (count pin 63 to 64).
- `go/internal/queryplan/testdata/statement-builders.yaml`: the always-empty
  read exemption for the fallback is removed. Statement coverage is Neo4j-only,
  and Neo4j no longer sends that read, so the exemption would fail as stale.
- `go/internal/queryplan/testdata/query-source-coverage.yaml`:
  `(*Handler).GetEntityContext` source digest re-pinned.
- `specs/live-tests.d/live-tests-02.yaml`: a `ci` row for
  `context_anchor_census_live_test.go` (Neo4j).

## NOT_CHECKED

- The census read on the production environment. This change must not merge
  until that read meets the gate.
- The golden-corpus statement-coverage and backend-diff phases on a real replay
  with this change. They run in CI only.
- The live tests on the repo's pinned `neo4j:2026-community` digest, and on
  NornicDB (the Neo4j-only test skips there; `TestLiveEntityContextUIDAnchor`
  runs on both in CI).
- Endpoint latency or p95 after the deploy. The warm sweep that closes #7212
  measures it.
- Nodes written between two census passes (class 4 in
  `7212-id-anchor-census.md`): such a node answers 404 on Neo4j until its writer
  is fixed.
