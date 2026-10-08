# Entity context: Neo4j anchor widened to every constrained label (#7212)

## Scope

This note covers the change that widens the Neo4j anchor statement of
`GET /api/v0/entities/{entity_id}/context` from the 15-entry
`EntityContextAnchorLabels` list to every schema label with a uid or id
uniqueness constraint. `buildNeo4jEntityContextAnchor` keeps the same two-branch
`CALL () { ... UNION ... }` shape, the same `anchor_rank` wrapper, and the same
statement tail. Only the label sets and the rank list grow.

The NornicDB path does not change. It still runs the per-label loop over
`EntityContextAnchorLabels` and then the unlabeled fallback. The Neo4j fallback
(`MATCH (e) WHERE e.id = $entity_id`) also stays, unchanged, as the second
statement.

#7212 stays open. The post-deploy step-1a sweep decides the close (see
[What decides the close](#what-decides-the-close)).

## What changed

- `graph.UIDUniquenessConstrainedLabels` and `graph.IDUniquenessConstrainedLabels`
  enumerate the labels that `graph.HasUIDUniquenessConstraint` and
  `graph.HasIDUniquenessConstraint` report true for. Both are sorted fresh
  slices. Tests pin them to the labels the schema DDL constrains: uid on both
  backends, id on Neo4j.
- `neo4jAnchorRankOrder` builds one deterministic order from those two sets.
  First come the 14 `EntityContextAnchorLabels` entries that carry a constraint,
  in the loop's order: Function, Class, Struct, Interface, TypeAlias, File,
  Repository, Module, Enum, Union, Macro, TypeAnnotation, Workload,
  WorkloadInstance. Then the other 110 constrained labels follow in alphabetical
  order. Directory has neither constraint, so the construction excludes it.
- `buildNeo4jEntityContextAnchor` puts each label in the uid branch if it has a
  uid constraint, else in the id branch. At this base that is 117 uid labels and
  7 id labels (Repository, Workload, WorkloadInstance, CloudAction, Endpoint,
  EvidenceArtifact, Platform): 124 labels in total, the two sets disjoint.
- An id shared by an old loop label and a widened label resolves to the old
  label, as the loop did. An id shared by two widened labels resolves to the
  first in alphabetical order. Before, the fallback returned an arbitrary first
  row.

The close-or-continue ruling (decision 7) counted 116 uid labels. The schema
tables at this base derive 117. The T1 shim rendered and measured the same 117,
so the measured width and the shipped width are equal.

## Byte identity with the measured statement

The T1 shim rendered its candidate with a generalization of this builder and
measured that text. Production now sends exactly that text.
`TestNeo4jEntityContextAnchorIsTheMeasuredStatement` pins the unscoped
`statements[0]` to the SHA-256 that the shim measured,
`e3a6d9edd0fc7ca56892c3fda781d397094b0c12b972b99a89e2f0917c72ed25`. It also
pins the anchor clause to the checked-in copy in
`go/internal/query/entity/testdata/neo4j_wide_anchor.cypher`.
`TestNeo4jEntityContextScopedShapeKeepsTheWideAnchor` pins the scoped shape: the
same clause, the grant filter on the Repository enrichment, and the unchanged
fallback. The bound parameters are `$entity_id` and the grant lists only.

## Baseline

The baseline is the image before this change, on ops-qa, 2026-10-07. A step-1a
warm sweep sent 20 calls for each cell and computed the nearest-rank p95:

| id class | API p95 | MCP p95 |
| --- | --- | --- |
| id on a label outside the old 15 (fallback-only) | 1.134 s | 1.148 s |
| absent id | 1.137 s | 1.202 s |
| id on an old anchor label | 0.109 s | 0.111 s |

On ops-qa, 29.7% of id-bearing nodes are fallback-only (263,344 of 886,253).
Most of them are on uid-constrained labels outside the old list. For each of
those ids, the shipped path sends two statements, and the second statement is an
AllNodesScan.

## Performance Evidence

Performance Evidence: a read-only ops-qa PROFILE (run 558cb48fdede) planned the widened anchor as 124 NodeUniqueIndexSeek operators with no scan, at 124 db hits on a miss, and answered fallback-only ids in 1 to 2 ms server time (result_consumed_after) against 940 to 1235 ms for the shipped fallback, with an identical row; the local cost bench (run cf7541cb485f, bar set B2, PASS) measured the cold compile at a 226 ms median and the cached-plan cost at +0.593 ms.

### Plan, miss cost, and row identity (ops-qa, read-only PROFILE)

Run 558cb48fdede, 2026-10-07, Neo4j Bolt read mode. It is not a latency sweep.
PROFILE inflates server time. There is one observation per round, in 3 rounds,
and the first mover alternates. Server time here is Neo4j's
`result_consumed_after`. The candidate anchor's warm `result_available_after`
was 0 to 31 ms, and 412 ms on its cold first execution.

- The plan is one NodeUniqueIndexSeek per label (124), with no NodeByLabelScan,
  AllNodesScan, or UnionNodeByLabelsScan.
- An absent key costs 124 db hits on the anchor.
- For a fallback-only id on each of the five most populated fallback-only
  labels, the anchor returns the same row as the fallback.

| label | candidate anchor, ms by round | shipped fallback, ms by round |
| --- | --- | --- |
| TerraformVariable | 2, 1, 1 | 1235, 1081, 1062 |
| TerraformResource | 2, 1, 1 | 1087, 1086, 1050 |
| TerraformLocal | 2, 1, 1 | 1085, 1090, 940 |
| Annotation | 2, 1, 1 | 1088, 1091, 945 |
| TerraformDataSource | 2, 2, 1 | 1090, 1079, 946 |

- The first execution of the widened statement, on a cold plan, reported
  412 ms server-available time. That is one sample, under PROFILE.
- An absent id still ends in the fallback scan. Per request, the anchor plus
  the fallback took about 0.93 to 1.08 s server time under PROFILE. By round
  (1, 2, 3), each figure is the anchor ms plus the fallback ms: shipped 1,079 /
  1,063 / 932 ms, candidate 1,083 / 1,070 / 932 ms. The three-round totals are
  3,074 ms and 3,085 ms. This change does not help a true miss.

### Compile and cached-plan cost (local bench B2)

Run cf7541cb485f, bar set B2, verdict PASS. The host was a laptop, with the
295-node fixture and the pinned Neo4j image. Each W round clears the plan cache
before the request, which measures the compile cost. Warm cells are interleaved
cached-plan reruns, timed in microseconds after the connection was verified.

| cell | measure | figure | bar |
| --- | --- | --- | --- |
| hit | cold t_first, candidate, 10 rounds | median 226 ms, max 353 ms | median <= 500 ms, every round <= 1000 ms |
| hit | cold t_first, paired diff candidate minus shipped | median +118 ms | information |
| miss | cold t_first, candidate, 10 rounds | median 153.5 ms, max 163 ms | same as hit |
| miss | cold t_first, paired diff candidate minus shipped | median +74 ms | information |
| hit | warm paired diff, n = 100 | median +0.593 ms | <= 1.5 ms |
| miss | warm paired diff, n = 100 | median +0.3005 ms | <= 1.5 ms |
| hit / miss | informational top-40-label cut, cold t_first median | 133 ms / 93 ms | none |

The bench used a cleared plan cache in every W round. It shows the compile cost
per plan epoch, not per request.

### Per-epoch model

Neo4j caches an executable plan for each statement text and set of parameter
types. This route sends at most four constant, parameterized texts per process:
the anchor and the fallback, each in the unscoped and the scoped shape. So the
compile above is paid once per text per plan epoch. A new epoch starts when:

- the process starts (a cold plan cache);
- the statistics of a planned label diverge past
  `dbms.cypher.statistics_divergence_threshold`, checked at most once each
  `dbms.cypher.min_replan_interval`;
- an index or a constraint is created, which clears the caches;
- the per-database query cache evicts the entry (1000 entries by default).

NOT_CHECKED: whether Eshu's idempotent schema ensure
(`CREATE ... IF NOT EXISTS`, when nothing changes) clears the plan caches. If it
does, each schema ensure starts an epoch.

### Hypothesis ledger

| candidate | expected saving | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | --- | --- | --- | --- | --- | --- | --- |
| widen the Neo4j anchor to all 124 constrained labels | about 1.1 s per fallback-only request | ops-qa PROFILE 558cb48fdede and local bench cf7541cb485f | 940 to 1235 ms (fallback, PROFILE, result_consumed_after) | 1 to 2 ms (anchor, PROFILE, result_consumed_after) | same row (P3) | read-only; no lock, queue, or worker | proven |
| ranked top-40 cut | smaller compile | same bench, informational | not applicable | 133 ms / 93 ms cold t_first | same row | read-only | rejected: labels below the cut keep paying about 1.1 s each request |

Classification: handler win for fallback-only ids, on the plan and server-time
level. No end-to-end or target claim. The next measured long pole is the
true-miss fallback scan (about 1.1 s per absent id at baseline).

## Observability Evidence

Observability Evidence: no new signal; the existing eshu_dp_entity_context_resolution_total{resolved_by} counter and the eshu.entity_context.resolved_by and eshu.entity_context.statements_tried span attributes (#7679) show the effect, because an id on a widened label now reads resolved_by=anchor with statements_tried=1 where it read resolved_by=fallback with statements_tried=2.

- After deploy, the `fallback` share of
  `eshu_dp_entity_context_resolution_total` must drop. The remaining `fallback`
  answers are nodes the seek cannot reach: a node with an id and no uid on a
  uid-constrained label, a node whose id differs from its uid, and a node on a
  label with no uid or id constraint (Directory).
- The `none` share is the true-miss share. This change does not move it. Each
  `none` request still runs the fallback scan.
- `TestNeo4jEntityContextResolvesFormerFallbackLabelsOnTheAnchor` reads the span
  and the counter. It pins `resolved_by=anchor` and `statements_tried=1` for a
  TerraformVariable id and an Endpoint id, and it pins that no fallback
  statement is sent.

## Live differential

`TestLiveNeo4jEntityContextAnchorMatchesLoop` (build tag
`live_nornicdb_answer_truth`, Neo4j leg only) compares each answer with the
per-label loop as the oracle. It gains these scenarios:

- a uid-constrained TerraformVariable (1 statement);
- an id-constrained Endpoint and CloudAction (1 statement);
- a TerraformVariable whose id differs from its uid (2 statements, the fallback);
- an id shared by WorkloadInstance and TerraformVariable, which resolves to
  WorkloadInstance.

Trait was already uid-constrained and outside the old list. It now resolves on
the anchor in 1 statement. The EXPLAIN check still forbids NodeByLabelScan,
AllNodesScan, and UnionNodeByLabelsScan. It now also requires a unique-index
seek on the right property for every label that the anchor names. The id-only
Function and the uid-only File fixtures keep their fallback expectations.

These live tests need Docker and Neo4j, and they run only in CI. They were
compiled and vetted with the tag (`go vet -tags live_nornicdb_answer_truth`) but
NOT_RUN locally.

## Reproduction

```
cd go && env -u GOROOT go test -p 2 -count=1 -run 'MeasuredStatement|ScopedShapeKeeps|SchemaConstraints|FormerFallback' ./internal/query/entity/
cd go && env -u GOROOT go test -p 2 -count=1 -run 'ConstrainedLabelsIsTheSchemaDDLSet' ./internal/graph/
cd go && env -u GOROOT go vet -tags live_nornicdb_answer_truth ./internal/query/entity/
```

## What decides the close

This change gates nothing about the close. After deploy, the step-1a sweep runs
again on the new image: 20 warm calls per cell, nearest-rank p95 at or below
1.000 s, on API and MCP, for the three id classes. The fallback-only cells are
expected to drop. The absent-id cells stay over 1 s until the true-miss work
(close-or-continue ruling, decision 8) lands. The `resolved_by` counter is how
the miss share is read. This note makes no endpoint latency or p95 claim for the
widened anchor.

NOT_CHECKED:

- The R (restart) and P (production) plan-epoch regimes on ops-qa: epoch
  frequency, replan events, and cache evictions.
- How many nodes the 110 extra labels hold on ops-qa. The PROFILE checked the
  five most populated fallback-only labels.
- The entity-context request mix on ops-qa (the fallback-only and miss shares).
- Whether the schema ensure clears the plan caches (see the per-epoch model).
