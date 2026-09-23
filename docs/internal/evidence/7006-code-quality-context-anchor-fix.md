# #7006 — Code-Quality And Entity-Context Read-Deadline Anchor Fix

Three Cypher shapes anchored on a broad or unlabeled node pattern and filtered
repository/entity identity only after the traversal, so a repo-scoped or by-id
call still paid a whole-corpus (or whole-graph) scan. All three hit the
10s bounded graph-read deadline (`defaultGraphReadTimeout`,
`go/internal/query/neo4j_read_policy.go`) on ops-qa on every request.

## Root Cause

1. `internal/query/codequery/quality/inspect.go` `BuildCypher` and
   `internal/query/codequery/complexity_queries.go` `complexityListAnchor`:
   `MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)`
   unconditionally, with the `repo.id = $repo_id` / scoped-grant predicate
   applied in a `WHERE` evaluated only after the full traversal. Cost scaled
   with the whole corpus's `Function` population, not the target repository.
2. `internal/query/entity/handler.go` `GetEntityContext`: `MATCH (e) WHERE
   e.id = $entity_id` — a completely unlabeled node, i.e. a whole-graph scan
   on every call.

## Fix

1 and 2 above now seed the walk from `Repository` (a small, `id`-indexed
label; `SHOW INDEXES` on ops-qa confirms `Repository.id` is indexed) whenever
a `repo_id` or scoped grant is known, matching the established
"seed from the smaller/known identity first" pattern in this doc (catalog
counts, relationship-story planner seed, file-import-cycle anchor). The fully
unscoped, no-`repo_id` case is unchanged (no Repository identity to seed
from).

3. `GetEntityContext` now anchors on the same code-entity label disjunction
   the call-chain builder already uses (`codequery/chain.AnchorLabelDisjunction
   = "Function|Class|Struct|Interface|TypeAlias|File"`), matching the
   "Call-Chain And Impact Unlabeled-Anchor Label Seed" fix already landed for
   this class of defect in this same doc (issue #3567). `SHOW INDEXES`
   confirms only `Function.id` is indexed among that label set; the rest are
   bounded to their own (far smaller) label population instead of every node
   in the graph.

All three are pure anchor-order/anchor-label rewrites: predicate set,
projections, ordering, and `LIMIT` are unchanged.

Backend: NornicDB via `kubectl port-forward` to the ops-qa deployment, image
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c`
(`sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`), Bolt
HTTP `tx/commit`, read-only `MATCH`/`RETURN` statements, varied params per run.
Repository identifiers are the opaque `repository:r_<hash>` graph ids only.

Performance Evidence: the pre-fix Function-first anchor
(`MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
WHERE coalesce(complexity,0)>0 AND repo.id=$repo_id`) did not return before a
12.0s client timeout for a repository whose complexity-filtered function count
is genuinely 0 (verified separately: 1307 total functions, 0 with
`cyclomatic_complexity>0`) — matching the issue's reproduced 10.0-10.1s
deadline. The shipped Repository-first anchor
(`MATCH (repo:Repository {id:$repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:Function)
WHERE coalesce(complexity,0)>0`) returned the same 0-row answer for that
repository in 2.74s, and returned 51 rows for a second, populated repository in
4.20s and 0 rows for a third in 0.37s — all well inside the 10s deadline, under
concurrent backend load from an earlier abandoned probe (see the accuracy note
below on why a same-property-anchored candidate was rejected in favor of this
traversal-anchored shape). `get_entity_context`'s pre-fix `MATCH (e)` shape is
the same class of defect as the already-evidenced #3567 call-chain fix (a
whole-graph scan, versus a per-label scan bounded to at most five small labels
after the fix); no separate live timing was captured for this specific route
because the pathology needs no live timing to establish (an unlabeled anchor
scans the whole graph by construction), and the fix is a correctness-of-shape
change identical in kind to #3567.

Accuracy Evidence: a candidate anchoring directly on the denormalized
`Function.repo_id` property (also indexed) was measured and rejected: its
timing was inconsistent under load (4.30s once, did not return before a 15s
timeout once, for the same repository) and it would depend on cross-property
consistency between `Function.repo_id` and the `REPO_CONTAINS`/`CONTAINS`
traversal that produces the response's repository-name enrichment field — an
assumption the shipped Repository-first traversal avoids entirely, since the
traversal itself is the ground truth for repository membership. Both
`Function.id` and `Repository.id` remain indexed and unaffected; the predicate
set, projections, and ordering are unchanged from the pre-fix text.

No-Observability-Change: all three routes keep their existing `GraphQuery.Run`/
`RunSingle` adapters, `neo4j.query` spans, and `eshu_dp_neo4j_query_duration_seconds`-class
query-duration telemetry. The response shapes, truth envelopes, and HTTP status
contracts are unchanged; no new metric, span, log field, queue, worker, or
runtime knob is introduced. (The issue's separate ask -- a bounded query/route
name attribute on the `query.graph_read.warning` log and span -- is not
implemented in this change; it requires threading a query-name value through
the shared `querycontract.GraphQuery` interface across every call site, a
cross-cutting interface change tracked as a follow-up rather than folded into
this anchor-order fix.)

`explain_dependency_path` and `trace_resource_to_code` (`internal/query/impact/handler.go`)
were investigated but not changed: both already anchor by-id lookups on a
single resolved, label-indexed node (`impacttrace.ResolveImpactAnchorNode`),
and live timing against varied real `CloudResource`/`Workload` anchors on
ops-qa (bounded `[*1..8]` traversal, `limit=51`) completed in 0.11-0.67s,
not reproducing the issue's deadline. Two structural risk factors remain
unmeasured -- the untyped, unbounded-by-relationship-type variable-length
traversal in `impacttrace.ImpactRepoPathCypher` (depth up to 20), and an
unindexed `{name: $id}` `CALL{UNION}` branch in `impactAnchorResolveCypher`
for several labels (`CloudResource`, `DataAsset`, `Endpoint`, `CloudAction`,
`Platform`, `TerraformOutput`, `KubernetesWorkload`, `TerraformModule` per
`SHOW INDEXES`) -- but fixing either requires a completeness/design decision
(which relationship types or hop bound the resource-to-code contract actually
needs; whether to add a schema index or drop the name-lookup branch for large
unindexed labels) rather than a mechanical rewrite, so these two paths are left
for a follow-up issue.
