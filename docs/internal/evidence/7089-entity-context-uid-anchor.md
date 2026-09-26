# #7089: Entity-context per-label reads anchor on uid

## Problem

`GET /api/v0/entities/{id}/context` and MCP `get_entity_context` took
0.75-1.42s end to end on the ops-qa Neo4j deployment. #7006/#7118
(`7006-code-quality-context-anchor-fix.md`,
`7006-live-answer-truth-fallback.md`) replaced the unlabeled
`MATCH (e) WHERE e.id = $entity_id` with a per-label loop over
`EntityContextAnchorLabels` plus an unlabeled fallback, but every per-label
statement still matched `e.id = $entity_id`. Neo4j gives the code-entity
labels a `<label>_uid_unique` constraint and no `id` index, so each of those
statements planned as a `NodeByLabelScan`. The first one alone, `Function`,
scanned every Function node, about 1.08M db hits.

## Fix

`entityContextAnchors` (`go/internal/query/entity/context_handler.go`) now
renders each per-label read of a label that
`graph.HasUIDUniquenessConstraint` reports as
`MATCH (e:<Label>) WHERE e.uid = $entity_id AND e.id = $entity_id`.
`HasUIDUniquenessConstraint` (`go/internal/graph/schema_tables.go`) reads
`uidConstraintLabels`, the same table the DDL loop turns into
`<label>_uid_unique` constraints on both backends. It is not a hand-kept list.
Of the 15 anchor labels, 11 are uid-constrained: Function, Class, Struct,
Interface, TypeAlias, File, Module, Enum, Union, Macro, TypeAnnotation.
Repository, Workload and WorkloadInstance are id-constrained, and Directory is
keyed by path. Those four keep `e.id = $entity_id`. The unlabeled fallback keeps
`e.id = $entity_id`. Loop order, the fallback, the shared deadline and the
projection are unchanged. The queryplan `GetEntityContext` source pin is
unchanged because the edit is in the helper, not the handler body.

The uid equality supplies the index seek. The id equality keeps the pre-#7089
match set exact: a clause can only return a node the old `e.id = $entity_id`
read returned. That matters for File, whose nodes all carry a uid and none an
id; without the id term a File uid would start resolving. Any node on a
uid-constrained label whose id differs from its uid (or has no uid) misses its
per-label read and still resolves through the unlabeled fallback. So the answer
cannot change, only which read finds it.

## Graph truth on ops-qa (read-only, 2026-09-25)

Neo4j Community 2026.08.1, 1,104,500 nodes. `SHOW CONSTRAINTS` matches the DDL
for the anchor labels. Per label, the count of nodes whose id is set but whose
uid is null or different, i.e. nodes the uid-anchored read would miss:

| label | nodes | with id | with uid | id set, uid null or != id | uid set, no id |
| --- | --- | --- | --- | --- | --- |
| Function | 539,931 | 539,931 | 539,931 | 0 | 0 |
| Class | 31,135 | 31,135 | 31,135 | 0 | 0 |
| Struct | 96 | 96 | 96 | 0 | 0 |
| Interface | 9,504 | 9,504 | 9,504 | 0 | 0 |
| TypeAlias | 12,859 | 12,859 | 12,859 | 0 | 0 |
| File | 143,841 | 0 | 143,841 | 0 | 143,841 |
| Module | 41,564 | 986 | 986 | 0 | 0 |
| Enum | 700 | 700 | 700 | 0 | 0 |
| Macro | 57 | 57 | 57 | 0 | 0 |
| TypeAnnotation | 23,956 | 23,956 | 23,956 | 0 | 0 |
| Union | 0 | 0 | 0 | 0 | 0 |

No node is lost to the fast path, and the File column is why the id term stays.

## Measurement

Method: the exact statements each build sends were rendered from the handler
(unscoped caller). The shipped text is byte-identical to the probed candidate
for all 16 statements. Each was run as `PROFILE` through
`cypher-shell --access-mode read`, simulating the loop per id (stop at the
first row, then the fallback). Two disjoint random id sets were used: 9 real
ids and 1 absent id each, 18 real ids in total. Before and after were
interleaved with an alternating first mover. Db hits are summed over the reads
the loop sends. Time is PROFILE server-side ms. The uid texts ran for the
first time in set A, so set A's "after" ms include planning; set B is warm.
Whether reprojection was writing at the same time was NOT_CHECKED. The db hits
of a hit read agree within about 20 between the two sets (Function
1,079,945-1,079,967), so the reads are comparable.

Performance Evidence: loop db hits and server ms, before (`e.id`) vs after
(uid anchor), per id; "reads" is how many statements the loop sent.

| label hit | set | reads | before db hits | before ms | after db hits | after ms |
| --- | --- | --- | --- | --- | --- | --- |
| Function (x3) | A | 1 | 1,079,945-1,079,954 | 469-550 | 85-94 | 0-1 |
| Function (x3) | B | 1 | 1,079,945-1,079,967 | 466-546 | 85-107 | 1 |
| Class | A / B | 2 | 1,142,218 / 1,142,235 | 581 / 557 | 88 / 105 | 85 / 0 |
| Struct | A / B | 3 | 1,142,428 / 1,142,415 | 483 / 504 | 106 / 93 | 60 / 2 |
| Interface | A / B | 4 | 1,161,415 / 1,161,422 | 679 / 490 | 85 / 92 | 177 / 3 |
| Repository | A / B | 7 | 1,482,239 / 1,476,002 | 706 / 630 | 7,507 / 1,270 | 7 / 7 |
| Module | A / B | 9 | 1,644,496 / 1,644,503 | 630 / 665 | 86,638 / 86,645 | 63 / 92 |
| TypeAnnotation | A / B | 13 | 1,693,926 / 1,693,926 | 1,401 / 834 | 86,642 / 86,642 | 633 / 91 |
| absent id | A / B | 16 | 3,902,850 / 3,902,850 | 2,075 / 1,643 | 2,295,564 / 2,295,564 | 1,321 / 1,001 |

Plans: every uid-anchored read is `NodeUniqueIndexSeek` on
`<label>_uid_unique` followed by `Filter cache[e.id] = $entity_id`. Every
pre-#7089 per-label read was a `NodeByLabelScan`, except Repository, Workload
and WorkloadInstance, which were already id seeks.

What is left after the fix: a hit at position 8 or later pays the Directory read,
a `NodeByLabelScan` over 43,274 Directory nodes (about 86.5k db hits). The
canonical writer (`canonicalNodeDirectoryNodeCypher`) keys Directory on `path`
and sets no `id`, and no ops-qa Directory node carries one, so that read
cannot match on this corpus. A miss still pays the unlabeled fallback, an
all-node scan (about 2.2M db hits), which #7118 kept for answer parity. Both
are follow-ups and not part of this change. The end-to-end API time after the
fix was NOT_CHECKED because it needs the change deployed. The figures above are
server-side PROFILE only.

Accuracy Evidence: for all 18 real ids, the loop returned the same row before
and after (SHA-1 of the full result row compared; ids and repository names are
not recorded here), and both absent ids return no row on both. Unit test
`TestGetEntityContextAnchorsUIDConstrainedLabelsOnUID` derives the expected
uid-constrained set from the Neo4j and NornicDB schema DDL and checks the
statements the production loop sends. It failed on main for all 11
uid-constrained labels. `TestHasUIDUniquenessConstraintMatchesSchemaDDL` pins
the predicate to both backends' DDL. Live test `TestLiveEntityContextUIDAnchor`
(`context_uid_anchor_live_test.go`, tag `live_nornicdb_answer_truth`, CI class)
passes on local `neo4j:2026-community` (the pinned
`docker-compose.live-backend-neo4j.yml` digest) with Eshu's schema applied. It
checks four things:
a canonical id == uid Function resolves on the first read; a File that matches
only by uid is a 404 after all 16 reads; an id-only Function resolves through
the fallback; and on Neo4j, `EXPLAIN` shows `NodeUniqueIndexSeek` on
`e:Function(uid)` for the shipped read and `NodeByLabelScan` for the old one.
It fails against the pre-fix handler, and against a uid-only anchor (the File
then resolves on read 6; the #6786 row-id guard still turns it into a 404, and
the read count catches it). NornicDB was not run locally. The live-backend CI
job runs the same test on NornicDB, where the two-predicate single `WHERE` is
the documented safe form (`nornicdb-pitfalls.md`, inline-pattern pitfall).

No-Observability-Change: the reads keep the `entity.context` graph query name,
the shared bounded deadline, the `neo4j.query` spans,
`eshu_dp_neo4j_query_duration_seconds`, the `query.graph_read.warning` log, and
the anchor-loop failure log with `labels_tried`. No metric, span, log field,
route, or response shape changes.
