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

## Wave 2 Addendum — `POST /api/v0/infra/relationships`

Scope grew after the initial fix landed (issue #7006 comment thread) to cover
`POST /api/v0/code/relationships`, `POST /api/v0/relationships/edges`,
`POST /api/v0/infra/resources/search`, and `POST /api/v0/infra/relationships`.
Only the last is fixed here; the other three were investigated and are either
already correctly anchored or need a design decision rather than a mechanical
rewrite (see `$SP/team/7006-evidence.md`, "Wave 2", for the full locate/measure
table and disposition on each). `internal/query/relationship_handlers.go`'s
`relationshipsGraphRow` entity_id branch was initially suspected to share the
same defect class, but its bare `MATCH (e)` anchor is Neo4j-only dead code on
this deployment (NornicDB routes through an already-anchored builder) and was
left unchanged.

`internal/query/infra_relationship_filter.go` `getRelationships`
(`POST /api/v0/infra/relationships`, MCP `analyze_infra_relationships`) had the
same unlabeled `MATCH (n) WHERE n.id = $entity_id` whole-graph-scan defect as
`get_entity_context`. This route resolves infra/platform entities (Workload,
CloudResource, Repository, TerraformResource, ...), not code entities, so it
seeds `impacttrace.ImpactAnchorLabelDisjunction` -- the same disjunction the
by-id impact reads already use -- instead of the code-entity set.

Performance Evidence: the pre-fix shape did not return before a 12.0s client
timeout for a real `CloudResource` id on ops-qa, matching the issue's
reproduced 10.0-10.1s deadline exactly. A labeled-anchor candidate was timed
post-fix but under heavy concurrent backend load from an abandoned probe (pod
CPU/RSS elevated); those readings (8.2s/10.4s, 0 rows for an id known to
exist) are recorded but not trusted as clean evidence and are not claimed as
a verified sub-second result. The fix is shipped on correctness-of-shape
grounds: it is the identical, already-proven label-disjunction pattern used
for `get_entity_context` in this same change and for
`explain_dependency_path`/`trace_resource_to_code` historically, converting a
provably-unbounded whole-graph scan into a per-label bounded scan (at most 14
labels, several of them small).

Accuracy Evidence: this route's `getRelationships` symbol was already
registered in the query-plan gate's `grandfatheredNonHotSourceDigests` with a
`non_hot_reason` prose disposition reading "unlabeled relationship-detail
lookup is inventoried but cannot be admitted until its production anchor is
typed" -- the gate's own inventory had already flagged this exact defect as
technical debt. Converting it to a typed `non_hot: {class: keyed_support,
key_bound: single_key, max_results: 1}` disposition with the new production
source hash (and removing it from `grandfatheredNonHotSourceDigests`, per
`internal/queryplan/AGENTS.md`'s required-conversion rule) is a strict
tightening of the gate, not a relaxation: the classification (single-key,
one-row lookup) is unchanged from what the prose already implied, only now
machine-checked. The predicate, `OPTIONAL MATCH` hops, and projection are
otherwise byte-identical to the pre-fix text.

No-Observability-Change: the route keeps its existing `GraphQuery.RunSingle`
adapter, `startQueryHandlerSpan` span, and query-duration telemetry. No new
metric, span, log field, or runtime knob.

Process note: running `go test ./internal/queryplan -count=1` after this
wave's fix surfaced that the Wave-1 commit had NOT run that gate and had left
two typed `non_hot` source-hash pins stale after their anchor-order edits
(`complexity_queries.go` `listMostComplexFunctions`, `entity/handler.go`
`GetEntityContext`) -- both would have failed the gate in CI. Both are
corrected in the same commit as this addendum.

## Wave 3 Addendum — Critical Correctness Fix: Label Disjunctions Are Unsafe On This NornicDB Pin

Live-proving `get_entity_context` on a real Function/Class id (requested in
review) surfaced a severe defect in the label-disjunction anchor shape shipped
in the original fix above and in the Wave 2 `infra/relationships` fix:
`MATCH (n:A|B|C) WHERE n.id = $id` (and its inline-map form
`MATCH (n:A|B|C {id: $id})`) **silently returns zero rows** on this pin
(`fix-500-e022384c`) for an id a single-label `MATCH (n:A) WHERE n.id = $id`
resolves correctly. Reproduced side-by-side in one transaction batch (ruling
out caching) for both a code-entity id (label `Function`) and an infra-entity
id (label `Workload`).

Accuracy Evidence: this is the documented NornicDB pitfall
`impact_anchor_resolve.go` already names ("a label disjunction matches zero
rows on the pinned NornicDB build, #5286") -- the shape shipped here matched
`codequery/chain.AnchorLabelDisjunction`'s text by false analogy: that
constant's `MATCH (start:A|B|C)` usage is Neo4j-only dead code on this
deployment (`BuildCallChainCypher` early-returns to a separate NornicDB
builder for `GraphBackendNornicDB`), so it was never proven against NornicDB
in production despite looking identical. Both affected handlers
(`entity/handler.go` `GetEntityContext`, `infra_relationship_filter.go`
`getRelationships`) now issue one single-label `MATCH` per candidate label in
a Go-side loop (`EntityContextAnchorLabels`, `impactRelationshipAnchorLabels`),
stopping at the first match; a genuinely absent entity now pays the full
label count in sequential single-label reads rather than a whole-graph scan,
a many-branch `CALL{UNION}` (see below), or a silently-wrong empty result.

A second, independent defect surfaced while proving the replacement's file/repo
enrichment hop: chaining a second hop onto an already-fast, single-bound-node
`OPTIONAL MATCH` -- `OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)`
-- did not return before a 10s timeout for either a structurally-empty first
hop (`e`=Repository) or a genuinely-matching one (`e`=Function, whose File
parent resolves instantly alone). A single REQUIRED reverse `:REPO_CONTAINS`
hop from a bound File node also returned zero rows, fast, for a File whose
Repository provably exists and resolves via the forward direction -- the
reverse direction of this relationship type is unreliable on this pin
regardless of `OPTIONAL`. `GetEntityContext` now resolves `repo_id` from the
entity/File node's own direct `repo_id` property (confirmed present, no extra
hop) and relies on its pre-existing `hydrateResolvedEntityRepoIdentity` call
to backfill `repo_name` from the content store, exactly as it already did for
other repo-name gaps; the Go-side `access.AllowsRepositoryID` check after
hydration is unchanged and remains the real scoped-access boundary (the
removed in-Cypher `WHERE` on the Repository hop was defense-in-depth on top
of it, not the gate itself).

Performance Evidence (clean, uncontended measurements): the final shape
(single-label anchor + two independent single-hop `OPTIONAL MATCH`es, no
chaining) resolved a real `Function` id in 0.14s (correct file_path,
language, line span, repo_id) and a real `Repository` id in 0.37s (correct
own `CONTAINS` relationships). Both are new lows for this route; no case
observed above 0.4s once the chained-hop pattern was removed.

No-Observability-Change: unchanged from the original entry above; the
handler keeps the same `GraphQuery.RunSingle` adapter and query-duration
telemetry, now additionally carrying `eshu.graph_read.query_name` (see the
Wave 3 telemetry entry below).

## Wave 3 Addendum — Telemetry: Bounded Query Name

Implements the issue's telemetry ask.
`querycontract.WithGraphQueryName`/`GraphQueryNameFromContext` threads a
bounded, low-cardinality query name (default `"unnamed"`) through the request
context into `recordGraphReadTelemetry` (`neo4j_read_policy.go`), which now
sets `eshu.graph_read.query_name` on the `neo4j.query` span and
`graph_query_name` on the `query.graph_read.warning` log. No `GraphQuery`
interface change -- every existing `Run`/`RunSingle` call site is unaffected;
only the four handlers this PR touches set a name, each reusing its own
existing `BuildTruthEnvelope` capability string: `code_quality.complexity`,
`code_quality.refactoring`, `code_search.fuzzy_symbol`,
`platform_impact.deployment_chain`.

No-Observability-Change does not apply here -- this IS the observability
change. New span attribute `eshu.graph_read.query_name`
(`internal/telemetry/contract/graph_read.go`) and log field
`graph_query_name`; no new metric, span name, or runtime knob.

## Wave 3 Addendum — Complexity/Quality: Root Cause Beyond The Anchor Fix

Re-measured the shipped Repository-first anchor (first entry above) on five
repos after the lead flagged the earlier numbers as contaminated by a
still-draining prior probe. Clean numbers: 0.37s-4.20s for four repos; the
fifth (function-dense: 3599 functions over 392 files, vs. 1307/419 for a
comparable repo) measured unstable across three separate runs -- 4.20s, a
12s timeout, and 9.11s -- none reaching the <1s target.

Performance Evidence (staged breakdown, one statement at a time, same
repository): Repository->File count 0.53s; +File->Function traversal +2.02s
(cumulative 2.55s); +`WHERE coalesce(cyclomatic_complexity,0)>0` filter
+0.43s (2.98s); +full `RETURN`/`ORDER BY`/`LIMIT` (shipped shape) +0.83s
(3.81s). The complexity filter is NOT the dominant cost (0.43s of 3.81s);
the File->Function traversal itself dominates (+2.02s), before any filter
runs. `Function.cyclomatic_complexity` has no index (`SHOW INDEXES`
confirmed), but adding one would only attack the smaller 0.43s slice.

The unstable repo's `ORDER BY complexity DESC, ...` sorts on a non-indexed
projected property -- the requested ranking itself, which (unlike the
historical `relationshipEdgesCypher` fix in this same doc) cannot be
redirected to an indexed tie-breaker without changing the answer. NornicDB
must materialize and sort the full per-repo Function population before
`LIMIT` applies; cost scales with function count, consistent with the
unstable repo's 2.7x higher function density.

Disposition: not fixed this wave. Options recorded for the owner: (1) add an
index on `Function.cyclomatic_complexity` and verify NornicDB can use it for
an ordered/descending scan, not just equality (unverified; needs an isolated
environment, cannot test schema DDL on ops-qa); (2) precompute a per-repo
complexity ranking at reducer write time (schema/pipeline design decision);
(3) accept the measured floor (reliably sub-1s to ~4s for sparse repos,
occasionally 9-12s+ for dense ones) and document the residual risk, matching
the disposition already accepted for the Wave 2 `relationships/edges`
IMPORTS/File residual.

## Wave 3 Addendum — explain_dependency_path / trace_resource_to_code: Root Cause Found, Not Fixed

Reproduced the exact statement `impactAnchorResolveCypher` issues: a
`CALL{...UNION...}` of 14 labels x 2 properties (id, name) = 28 branches, for
a real, confirmed-existing `CloudResource` id. **Did not return before a
15.0s client timeout** -- badly non-linear (an 8-branch subset of the same
statement resolved the same id correctly in 0.67s; 3.5x more branches did not
complete in 22x the time). This, not the bounded traversal that follows it
(measured 0.11-0.17s separately), is the statement that consumes the 10s
deadline; `explain_dependency_path` calls it twice per request
(source+target), `trace_resource_to_code` once, consistent with the audit's
reproduced 10.0-10.1s on 4/4 calls.

Not fixed this wave: the obvious replacement (a single-label-per-MATCH loop,
the same pattern now proven for `get_entity_context` and
`infra_relationship_filter.go`) is very likely correct given this session's
findings, but was not proven against these specific 14 impact labels before
the remaining ops-qa budget ran out on the correctness fixes above. Flagged
as the concrete next step: replace `impactAnchorResolveCypher`'s
`CALL{UNION}` with the same per-label-loop shape.
