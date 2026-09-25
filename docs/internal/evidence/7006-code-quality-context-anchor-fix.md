# #7006 — Code-Quality And Entity-Context Read-Deadline Anchor Fix

Three Cypher shapes anchored on a broad or unlabeled node pattern and filtered
repository/entity identity only after the traversal, so a repo-scoped or by-id
call still paid a whole-corpus (or whole-graph) scan. **On the NornicDB
backend ops-qa ran until 2026-09-24**, all three hit the 10s bounded
graph-read deadline (`defaultGraphReadTimeout`,
`go/internal/query/neo4j_read_policy.go`) on every request.

**Scope of the latency claim.** ops-qa now runs Neo4j, where the deployed
statements were already under 1.5s and nothing timed out (last section).
There the complexity list and `code/quality/inspect` rewrites are a no-op
(identical db hits, byte-identical rows), `infra/relationships` is about 10x
to several hundred x faster, `get_entity_context` about 2x, and the query-name
telemetry and shared deadline apply on both backends. The 10s fix is
NornicDB-specific; read the sections below with that scope.

## Root Cause

1. `internal/query/codequery/quality/inspect.go` `BuildCypher` and
   `internal/query/codequery/complexity_queries.go` `complexityListAnchor`:
   `MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)`
   unconditionally, with the `repo.id = $repo_id` / scoped-grant predicate
   applied in a `WHERE` evaluated only after the full traversal. Cost scaled
   with the whole corpus's `Function` population, not the target repository.
2. `internal/query/entity/handler.go` `GetEntityContext` (later split into
   `internal/query/entity/context_handler.go` to keep `handler.go` under the
   500-line cap; same symbol, same fix): `MATCH (e) WHERE e.id = $entity_id`
   — a completely unlabeled node, i.e. a whole-graph scan on every call.

## Fix

1 and 2 above now seed the walk from `Repository` (a small, `id`-indexed
label; `SHOW INDEXES` on ops-qa confirms `Repository.id` is indexed) whenever
a `repo_id` or scoped grant is known, matching the established
"seed from the smaller/known identity first" pattern in this doc (catalog
counts, relationship-story planner seed, file-import-cycle anchor). The fully
unscoped, no-`repo_id` case is unchanged (no Repository identity to seed
from).

3. `GetEntityContext` (`internal/query/entity/context_handler.go`) anchors
   with one single-label `MATCH (e:<Label>) WHERE e.id = $entity_id` per
   candidate in `EntityContextAnchorLabels`, most-common-first, stopping at
   the first row, then falls back to the pre-fix unlabeled read (Wave 5), all
   under one shared bounded deadline. A first attempt used
   a single label disjunction (`Function|Class|...`, as the call-chain builder
   does, issue #3567); the Wave 3 addendum proves that shape silently returns
   zero rows on the pinned NornicDB build, so it was replaced. See the Wave 3
   and Wave 4 addenda for the proof and the shared-deadline design.

1 and 2 (the complexity list and quality inspect anchors) are pure
anchor-order rewrites: predicate set, projections, ordering, and `LIMIT` are
unchanged. `GetEntityContext` (3) is an anchor swap plus fallback: every read
projects exactly the pre-fix columns from the pre-fix enrichment (Wave 5
restored the Repository hop that Wave 3 had replaced).

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
single resolved, label-indexed node (`deployment.ResolveImpactAnchorNode`,
`internal/query/impact/deployment` since #7034's package move),
and live timing against varied real `CloudResource`/`Workload` anchors on
ops-qa (bounded `[*1..8]` traversal, `limit=51`) completed in 0.11-0.67s,
not reproducing the issue's deadline. Two structural risk factors remain
unmeasured -- the untyped, unbounded-by-relationship-type variable-length
traversal in `deployment.ImpactRepoPathCypher` (depth up to 20), and an
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
rewrite. `internal/query/relationship_handlers.go`'s
`relationshipsGraphRow` entity_id branch was initially suspected to share the
same defect class, but its bare `MATCH (e)` anchor is Neo4j-only dead code on
this deployment (NornicDB routes through an already-anchored builder) and was
left unchanged.

`internal/query/infra_relationship_filter.go` `getRelationships`
(`POST /api/v0/infra/relationships`, MCP `analyze_infra_relationships`) had the
same unlabeled `MATCH (n) WHERE n.id = $entity_id` whole-graph-scan defect as
`get_entity_context`. This route resolves infra/platform entities (Workload,
CloudResource, Repository, TerraformResource, ...), not code entities, so it
seeds `deployment.ImpactAnchorLabelDisjunction` -- the same disjunction the
by-id impact reads already use -- instead of the code-entity set.

Performance Evidence: the pre-fix shape did not return before a 12.0s client
timeout for a real `CloudResource` id on ops-qa, matching the issue's
reproduced 10.0-10.1s deadline exactly. A labeled-anchor candidate was timed
post-fix but under heavy concurrent backend load from an abandoned probe (pod
CPU/RSS elevated); those readings (8.2s/10.4s, 0 rows for an id known to
exist) are recorded but not trusted as clean evidence and are not claimed as
a verified sub-second result. **Review correction (#7006 review round 3, F3):**
those readings are of the label-DISJUNCTION shape (`MATCH (n:A|B|...)`), the
one Wave 3 below proved silently returns zero rows on this pin and replaced
with the per-label loop -- not the shipped per-label-loop shape. They were
never valid latency evidence for what actually shipped, on top of already
being disclaimed as contended. The shipped per-label-loop shape was measured
on the ops-qa Neo4j backend that replaced NornicDB (last section), not on
NornicDB. Proven: correctness (Accuracy Evidence below; the Wave 3 addendum's
live per-label-loop reproduction on a real `Workload` id) and a hard upper
bound -- every call is capped by the shared 10s deadline (Wave 4 addendum).

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

Observability note: true when this addendum was written, no longer true --
later waves (below) add `eshu.graph_read.query_name`/`graph_query_name` (Wave
3 telemetry), a shared-deadline bound with its own `eshu.entity_anchor_labels_tried`
span attribute (Wave 4), and a graph-read-policy-deadline classification fix
(Wave 4 review round) to this exact route. See those addenda for the current
observability surface; this line is left for the historical record of what
Wave 2 alone changed (nothing, at that point).

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
regardless of `OPTIONAL`. Wave 3 therefore read `repo_id` from the node's own
`repo_id` property and dropped `repo_name`. **Superseded by Wave 5**: that
shape lost `repo_id`/`repo_name` wherever the nodes carry no `repo_id`
property and was reverted to the pre-fix Repository hop.

Performance Evidence (Wave 3 shape, not shipped after Wave 5; measured
before Wave 5): the Wave 3 statement (single-label anchor + two single-hop
`OPTIONAL MATCH`es, `repo_id` from the node property, no `repo_name`)
resolved a real `Function` id in 0.14s and a `Repository` id in 0.37s. Not
re-measured for the shipped shape.

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
only the four handlers this PR touches set a name: `code_quality.complexity`,
`code_quality.refactoring`, `platform_impact.deployment_chain` reuse their own
existing `BuildTruthEnvelope` capability string. `get_entity_context` did too
at first (`code_search.fuzzy_symbol`) but that made its deadline telemetry
indistinguishable from fuzzy-symbol search's; #7006 review round 3 (F6) gave
it its own `entity.context` name instead (see Wave 4 addendum).

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

## Wave 4 Addendum — Shared Deadline For The Per-Label Loops

A follow-up review found the Wave 3 per-label loop (`GetEntityContext`,
`getRelationships`) had no SHARED time budget: `Neo4jReader.runRead` gives
each `RunSingle` call its own fresh 10s window, so a genuine miss (or a match
on a late-tried label) could pay up to 14 x 10s -- worse than the original
whole-graph-scan defect this issue set out to fix. Both loops now derive ONE
shared deadline before the first iteration
(`querycontract.WithBoundedGraphReadDeadline`, the same 10s budget a single
statement gets) and reuse it for every candidate label; a spent budget
surfaces as the existing bounded-read 504 deadline response, never a silent
not-found or an unrelated 500. `GetEntityContext` logs `labels_tried`/
`labels_total` on any error path, and `getRelationships`' request span
records `eshu.entity_anchor_labels_tried`. `GetEntityContext` moved out of
`entity/handler.go` into `entity/context_handler.go` to keep `handler.go`
under the 500-line cap the fix grew it past.

TDD: `TestGetEntityContextAnchorLoopSharesOneDeadlineAcrossLabels`,
`TestGetEntityContextTranslatesASpentSharedBudgetToDeadlineResponse`,
`TestInfraRelationshipsAnchorLoopSharesOneDeadlineAcrossLabels`,
`TestInfraRelationshipsTranslatesASpentSharedBudgetToDeadlineResponse`,
`TestInfraRelationshipsSpanRecordsLabelsTried` -- each written and shown RED
against the pre-fix code, then GREEN after.

## Wave 4 Addendum — Review Round 3: Deadline Misclassification (F1), Query Name (F6)

**F1 (P1, fix-induced by the shared-deadline commit above).** The shared ctx
`WithBoundedGraphReadDeadline` creates is the PARENT of every `RunSingle`'s
own per-read `context.WithTimeout` inside `runRead`, created a few
microseconds earlier with the same 10s budget, so it always expires first.
`graphReadResult` checked `parentCtx.Err()` before anything else and
unconditionally classified that as `graphReadOutcomeCallerDeadline` -- never
`deadline`. Effect: every timeout on `GET /entities/{id}/context` and
`POST /infra/relationships` lost its `outcome="deadline"` metric label, its
`query.graph_read.warning` log (and therefore its `graph_query_name`), and
callers got a raw `context.DeadlineExceeded` instead of the wrapped
`ErrGraphReadDeadline` sentinel. This is the exact telemetry gap issue #7006
asked to close, silently reopened by the fix that closed the round-1 P1.

Fix: `querycontract.WithBoundedGraphReadDeadline(For)` now marks its returned
context with an internal key; `graphReadResult` checks that marker before
falling into the ordinary caller-deadline branch, and treats a marked,
expired parent context as the graph-read policy's own deadline --
`graphReadOutcomeDeadline` with the wrapped `ErrGraphReadDeadline`. An
ordinary caller deadline (one that never went through
`WithBoundedGraphReadDeadline`, e.g. an MCP dispatch timeout, or a test's own
`context.WithTimeout`) is unaffected and still classifies as
`caller_deadline` -- pinned by the pre-existing
`TestNeo4jReaderParentDeadlineDoesNotRecordPolicyDeadlineOutcome`, which
still passes unchanged.

TDD: `TestNeo4jReaderSharedBoundedDeadlineClassifiesAsPolicyDeadline`, through
the REAL `Neo4jReader` and its policy-test fake session (not a fake
`GraphQuery`, since only the real reader can misclassify this), shown RED
against the pre-fix `graphReadResult`, then GREEN. A new
`querycontract.WithBoundedGraphReadDeadlineFor(ctx, budget)` variant lets the
test reproduce the same parent-created-microseconds-before-child-deadline
race deterministically in milliseconds instead of waiting out the real 10s
budget twice.

**F6 (P3).** `GetEntityContext`'s `WithGraphQueryName` call reused the
`code_search.fuzzy_symbol` capability string (still used, unchanged, for
`WriteGraphReadError`/`CapabilityUnsupported` -- that string is a registered
capability in `specs/capability-matrix.v1.yaml` and must not change), making
its deadline telemetry indistinguishable from fuzzy-symbol search's. Now
tagged `entity.context`. The queryplan manifest's `listMostComplexFunctions`
entry documents (does not reclassify) that its `label: Function` covers only
the unscoped-no-`repo_id` worst case; the scoped/`repo_id` path anchors on
Repository first, a materially cheaper shape the single `non_hot` entry has
no separate slot for.

Observability: `eshu.graph_read.query_name` and `graph_query_name` gained a
proper `telemetry.LogKeyGraphReadQueryName` constant
(`internal/telemetry/contract/graph_read.go`), replacing the bare string
literal, and are now documented in
`docs/public/reference/telemetry/{graph-read-safety,traces}.md` alongside the
shared-deadline-loop behavior above (issue #7006 review round 3, F2).

**F3 (measurement gap).** Neither `infra/relationships`' shipped per-label
loop nor `code/quality/inspect` had a post-fix measurement; earlier claims
borrowed numbers from other shapes. Closed by the ops-qa Neo4j measurement
(last section). The 10s ceiling holds: the loop is bounded by the shared
`WithBoundedGraphReadDeadline` budget; `code/quality/inspect` is one statement
bounded at 10s by `Neo4jReader`'s own per-read policy.

**F4 (#7014 proof).** The previously-cited proof was uncommitted and the
wrong shape (a single-hop rejoin ordered by the rejoined variable, not this
PR's two-hop chain ordered by the end node). This session wrote and ran a new
test, `TestEshu7014ChainedShape`, against the PR's actual
chained-and-ordered-by-end-node shape
(`MATCH (repo:Repository) WHERE ... MATCH (repo)-[:REPO_CONTAINS]->(f:File)
-[:CONTAINS]->(e:Function) ... ORDER BY <e property> LIMIT $n`, LIMIT swept
1..21) on three commits, each run with
`env -u GOROOT go test -tags nolocalllm ./pkg/cypher/ -run TestEshu7014ChainedShape -v`
from the matching worktree under `~/os-repos/NornicDB-worktrees/`:

- `6ac958a9` (`7014-verify-6ac958a9`, this drive's pin base, before #7014's
  regressing commit): PASS.
- `f2163176` (`7014-verify-f2163176`, upstream `main`, after #7014's fix):
  PASS.
- `c4de1c5c` (`build-c4de1c5c`, the commit that introduced #7014 itself, not
  previously tested against in this drive): **PASS** -- the chained shape is
  not exposed even on the regressing commit, because it always carries
  `ORDER BY`.

Sensitivity check (a no-`ORDER BY` mutant of the same chained query, run then
discarded, not part of the citable proof): PASS on `6ac958a9` and `f2163176`,
**FAIL on `c4de1c5c`** (`LIMIT` 1-16 each dropped the one matching row, per
the #7006 review round 4 rerun; an earlier run of this same mutant reported
1-11, which this session did not reproduce) --
confirms the harness genuinely detects the #7014 drop when its precondition
(`ORDER BY` absent) holds, and that `ORDER BY` protects the chained shape too,
not just the single-hop-rejoin shape the earlier test covers. The test file
itself is left uncommitted in each of those three worktrees, matching the
existing ad hoc verification pattern already used there for #7014; it is not
part of this repo's own commit history.

## ops-qa Neo4j measurement (2026-09-24)

Closes review round 3 F3. ops-qa moved from NornicDB to Neo4j Community
`2026.08.1` on 2026-09-24, so the NornicDB timings above cannot be re-taken;
this measures all four rewritten routes on the current backend.

Method: before = the deployed image `sha-763c65e` (main `763c65e552`, no
#7006); after = the statements this branch sends. Both were extracted by
driving the real handlers with a recording `GraphQuery` over a `git archive`
of each commit, so Cypher and params are exactly what each build sends (the
two loop routes record all 14 per-label statements in loop order), then run as
`PROFILE` through `cypher-shell --access-mode read` (read-only), 3 repetitions;
the before side was also timed over the API, 2 passes. Graph: 921,615 nodes,
445,784 `Function`.

Caveat: a reprojection drain was running (Postgres IO-bound, Neo4j under write
load), so absolute times are from a moving, partly populated graph. PROFILE
`Time` is server-side; API seconds add network, auth and content hydration.
Db-hit counts and ratios are the load-independent part.

Performance Evidence: on Neo4j the deployed statements were already under
1.5s (API max 1.42s, PROFILE max 0.81s) against the 10s deadline.
`code/complexity` list and `code/quality/inspect`, 6 repos each (0 to 39.6k
complexity>0 functions): before 0.09-0.46s API, 0-262ms PROFILE; after
0-313ms; db hits identical on all 12 pairs (16-447k). `EXPLAIN` of the
deployed statement is `NodeUniqueIndexSeek` (Repository) -> `Expand` ->
`Expand`, so Neo4j already seeds from the Repository index and the rewrite is
a no-op here. `get_entity_context` (**Wave 3 shape; no longer the shipped Cypher, see Wave 5**), 6 real ids (3 Function, Class, Struct, Repository): before 0.75-1.42s API,
715-808ms PROFILE, 1.84-1.89M db hits; after (loop to first hit, 1-7 tries)
405-513ms, 0.89-1.22M db hits; a miss (14 tries) 551-594ms vs 690ms.
`infra/relationships`, 6 real ids: before 0.74-1.16s API, 754-797ms PROFILE,
1.84M db hits; after 0-59ms, 366-108k db hits (1-11 tries); a miss 72-74ms vs
776-783ms (**the miss figure no longer holds after Wave 5**: a miss now also
runs the unlabeled fallback, i.e. the "before" statement).

Accuracy Evidence: `infra/relationships` raw output is byte-identical between
the deployed and first-hit per-label statements for all 6 ids; for
`get_entity_context` every column matched except `repo_name` in the Wave 3
shape, which Wave 5 reverted; an absent id returns no row on both. One repo first showed a 2-db-hit mismatch across runs: live reprojection
drift, byte-identical on a back-to-back re-run.

Reading: nothing timed out before the fix on Neo4j (scope stated in the
intro). `infra/relationships` first-hit reads improve about 10x to several
hundred x in server time (0.78s to 0-59ms, measured at `bee2ff4a14`; that
statement is unchanged since). Another label or a miss now costs the
fast-path reads plus the pre-fix unlabeled statement (776-783 ms), slower
than pre-fix. `get_entity_context` has no valid after figure: the 1.2x-2x
measured the Wave 3 shape, replaced in Wave 5 and not re-measured. Its first
label is still a 445,784-node `NodeByLabelScan` (Neo4j indexes `Function.uid`,
not `Function.id`; the `uid` anchor is follow-up #7089). Complexity/inspect
show no change.

No-Observability-Change: this section adds measurement only; no code, metric,
span, or log field changed.


## Wave 5 Addendum — Answer-Truth Regressions Found By live-backend CI

Moved to [7006-live-answer-truth-fallback.md](7006-live-answer-truth-fallback.md)
to keep this record under the Markdown line cap: the per-label loops now end
with the pre-fix unlabeled read, and `GetEntityContext` projects the pre-fix
Repository-hop enrichment again.
