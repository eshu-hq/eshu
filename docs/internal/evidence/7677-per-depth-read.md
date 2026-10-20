# Change-surface reads: order-preserving per-depth iterative deepening (#7677)

The outgoing and repository-consumers impact traversals behind
`POST /api/v0/impact/change-surface`, `/change-surface/investigate` and the
MCP `find_change_surface` tool enumerated every path of up to max depth from
the anchor in one `[*1..max]` statement before ORDER/LIMIT. On the filed
anchor (`iac-eks-argocd`) that is 190,405 pre-LIMIT whitelisted paths
(depths 120 / 863 / 18,485 / 169,849 on the shared QA graph) for a 12-row probe page, all
depth 1. Each direction now reads one exact depth per `[*d..d]` statement and
merges depth-major with the remaining probe need, stopping when a depth fills
the limit+1 probe. The merged page equals the old
`ORDER BY depth, name, id` page (depth is the major sort key) with identical
`truncated`, while a shallow probe-full page never expands the deep fan-out.
No `ORDER BY` change, no early `LIMIT`, no pre-`LIMIT` dedup: the arbiter's
order-preserving allowance only.

Performance Evidence: query shapes `MATCH path = %s-[*d..d]->(impacted)
WHERE ... RETURN ... ORDER BY name, id LIMIT $limit` (unscoped),
the same wrapper per depth for the CALL-subquery UNION scoped shape, and
`MATCH path = (start:Repository {id: $target_id})<-[:DEPENDS_ON*d..d]-
(impacted:Repository) ... ORDER BY name, id LIMIT need` (consumers),
backend Neo4j on the shared QA graph (802 repositories), anchor
`NodeUniqueIndexSeek` on `Repository.id`. Filed anchor, unscoped, limit 11:
baseline `[*1..4]` wall 7261/6950/7843 ms per cypher-shell invocation
(~2070 ms client overhead), PROFILE Time 4004 ms, DbHits 21,714,414, 12 rows.
Per-depth depth-1 `[*1..1]` wall 2198/2003/2005 ms (overhead floor), PROFILE
Time 38 ms, DbHits 1,899, identical ordered (depth, name, id) sequence and
identical truncated=true (tie order among equal (depth, name, id) paths may
differ; the contract leaves it unspecified). Query-time reduction ~99%,
DbHits reduction >99.99%. Per-depth slice equality: depths 1..4 counts
120/863/18485/169849 equal both shapes with md5-identical ordered (name, id)
sequences; two further anchors plus the consumers branch show ordered-key
page equality; a d1-exhausted anchor shows the d1+d2 interleave merge equals
the baseline page. Small anchors issue up to maxDepth cheap statements
instead of one; each is milliseconds (depth-1 PROFILE above), so the added
round trips are noise against the removed worst case.

Observability Evidence: no new telemetry. Every per-depth statement reports
the existing `graph_query_name=platform_impact.change_surface.outgoing` name
on the `query.graph_read.warning` log and the `eshu.graph_read.query_name`
span attribute (pinned by
`TestChangeSurfaceOutgoingTraversalIsNamedForGraphReadTelemetry`); the
consumers branch stays unnamed by design (pinned by
`TestChangeSurfaceRepositoryTargetNamesOnlyTheOutgoingTraversal`). One
behavioral note for operators: a single change-surface read now emits up to
maxDepth graph statements (fewer when the probe fills early) instead of
exactly two, so per-statement warning counts scale with depth, not with work.

## NornicDB re-proof status

The `[*d..d]` bracket keeps the #6786 pairing exemption (one
variable-length relationship; the guard rejects only bracketless
fixed-length hops), proven statically for every depth 1..8 by
`TestChangeSurfaceDepthStatementsAreNornicDBSafe` with zero guard-source
changes. Live NornicDB proof was not runnable from this lane (no NornicDB
service or Docker in the environment): it rides the existing live legs
(`TestLiveChangeSurfaceLabelPredicate`,
`TestLiveChangeSurfaceLabelConjunctDeepTraversal`,
`TestLiveChangeSurfaceImpactTraversal`), which drive the rewritten
`RunChangeSurfaceOutgoing` / `RunChangeSurfaceRepositoryConsumers` directly
and must go green in CI before merge.

## Raw shim numbers (shared QA graph, read-only cypher-shell, 2026-10-20)

- Baseline page (LIMIT 12): 7261, 6950, 7843 ms wall; PROFILE 4004 ms /
  21,714,414 DbHits / 12 rows.
- Depth-1 page (LIMIT 12): 2198, 2003, 2005 ms wall; PROFILE 38 ms /
  1,899 DbHits / 12 rows; ordered (depth, name, id) sequence identical,
  truncated identical.
- Slice counts [*d..d] vs [*1..4]+length filter: 863=863,
  18485=18485, 169849=169849; ordered (name, id) md5 identical at all depths.
- Anchors B2 (`terraform-stack-node10`), B3 (`helm-charts`): ordered-key
  page equality, truncated=true both sides.
- Anchor C (11 whitelisted depth-1 paths): d1 (11 rows) + d2 LIMIT 1 merge
  equals the baseline 12-row page.
- Scoped CALL-UNION `[*1..1]`: 3 rows, the granted subset of the unscoped
  depth-1 page.
- Consumers anchor (72 incoming): 12-row ordered-key page equality.
