# NornicDB Query-Shape Pitfalls Reference

This page is the query-shape companion to
[NornicDB Behavior and Pitfalls](nornicdb-pitfalls.md) (which covers
storage, schema, constraint, and transaction behavior). It records Cypher
**shapes** that the pinned NornicDB planner/interpreter mishandles — label
disjunctions, empty-first-branch unions, outer aggregation over `CALL {}`, and
multi-clause reads — so a read or retract that looks correct does not silently
return wrong rows.

Use it to avoid rediscovering the same failure shape. Still check the current
NornicDB source before patching.

## How To Use This Page

1. Read the matching section before writing or changing a Cypher read/retract
   shape that anchors on a label disjunction, unions per-branch, aggregates over
   a `CALL {}` subquery, or places any clause between the anchor `MATCH` and the
   final `RETURN`.
2. Validate the behavior against the current `NornicDB-New` checkout that built
   the image under test.
3. Check upstream docs and release notes for the pinned `NORNICDB_IMAGE`.
4. If the current reproduction differs, update this page with the reproduction,
   observed shape, and either the root cause or open question.

NornicDB changes quickly. A documented behavior may already be fixed in the
binary you are testing.

## Pitfall: Node-Label Disjunction In A `MATCH` Matches Zero Rows

### Observed shape

Two related matching quirks make an all-source-labels retract unreliable.
Measured on the canonical `timothyswt/nornicdb-cpu-bge:v1.1.11` (and, for the
disjunction, `v1.1.9`):

```cypher
-- 1. Node-label disjunction matches zero rows (v1.1.9 and v1.1.11):
MATCH (s:Function)-[r:CALLS]->() RETURN count(r)         // 1
MATCH (s:Function|Class)-[r:CALLS]->() RETURN count(r)   // 0  (broken)
MATCH (s:Function|Class {uid: $u}) RETURN count(s)       // 0  (broken, even with a property key)

-- 2. Unlabeled source scan is unreliable on v1.1.11 (regression from v1.1.9):
MATCH (s)-[r:CALLS|REFERENCES|INSTANTIATES]->() WHERE s.repo_id IN $ids RETURN count(r)
--   drops some source labels (e.g. a File-sourced REFERENCES), inconsistently
--   by internal label-iteration state. On v1.1.9 the same query is complete.
```

Relationship-type disjunction (`-[r:CALLS|REFERENCES]->`) works on both. Only the
source **node** anchor is affected. A third quirk compounds it: on v1.1.11
multiple `DELETE` statements sharing a single managed Bolt transaction do not all
apply — a grouped per-label retract leaves some edges behind, while the same
statements run as separate auto-commit transactions delete every edge.

### Eshu implications

A retract that anchors its source on a label disjunction, or on an unlabeled
`(source)` scan, silently under-deletes on NornicDB. Issue #5116 was exactly
this: the code-call edge retract matched
`(source:Function|Class|Struct|Interface|TypeAlias|File)`, so it deleted zero
edges and stale `CALLS`/`REFERENCES`/`INSTANTIATES` edges survived every
reprojection.

The only shape that reliably retracts every source on both pinned versions is a
**single label per statement**, so the fix fans the retract out to one statement
per source label (`MATCH (source:Function)-[rel:CALLS|REFERENCES|INSTANTIATES]->()
WHERE source.repo_id IN $repo_ids AND rel.evidence_source = $evidence_source
DELETE rel`, repeated for `Class`/`Struct`/`Interface`/`TypeAlias`/`File`). The
statements run **sequentially** (each in its own transaction), not grouped,
because of the managed-transaction quirk above. Each statement is independently
scoped and idempotent, so sequential execution is safe. See
`buildCodeCallRetractStatements` and `executeCodeCallRetractStatements` in
`go/internal/storage/cypher`.

Do not resolve this by dropping the label to an unlabeled `(source)` scan (it
passes on v1.1.9 but silently under-deletes on v1.1.11), and do not group the
per-label statements into one transaction. A sibling instance of the same
anti-pattern is still open and tracked in #5116: the write-path fallback
templates (`batchCanonicalCodeCallUpsertCypher` and friends) silently write
nothing for unresolved-label endpoints. The inheritance retract carried the
same node-label disjunction and was fixed the same way in #4367
(`buildInheritanceRetractStatements`). The SQL-relationship retract carried
both remaining shapes: its per-label statements ran grouped through one
managed transaction (measured on v1.1.11: the first DELETE never applied,
deterministically across runs — sequenced in #5128, live proof
`TestReducerSQLRelationshipRetractGraphTruth`), and its non-GroupExecutor
fallback was an unlabeled `(source)` scan, removed in #4367 when the retract
moved to one statement per write-capable source label
(`buildSQLRelationshipRetractStatements`). The rationale EXPLAINS delta
retract carried the disjunction on its TARGET node
(`...->(target:Function|Class|...|File) WHERE target.path IN ...`) — probed on
v1.1.11: it deletes nothing — and was fixed the same way in #4367
(`BuildRetractRationaleEdgeStatementsByFilePath`, one statement per target
label, sequential, live proof `TestReducerRationaleEdgeRetractGraphTruth`).
Each remaining instance needs the same per-label + sequential rework with its
own live proof.

Further managed-transaction refinement (probed while fixing the
TAINT_FLOWS_TO retract): even a SINGLE `DELETE` statement dispatched through a
managed transaction (`ExecuteGroup`) can fail to apply on v1.1.11 — the same
statement auto-committed deletes the edge. Treat every retract `DELETE` as
auto-commit-only. Grouped dispatch also silently drops the SQL relationship
writer's `UNWIND`/`MATCH`/`MERGE` statement on v1.1.11, even with a one-statement
group; the identical auto-commit statement persists the edge. Treat grouped
execution as shape-specific and require live persistence proof before enabling
it, rather than assuming MERGE makes a managed transaction safe.

Two orphan-cleanup shapes are also broken on v1.1.11 (probed while fixing the
shell-exec cleanup): a negated pattern predicate (`WHERE NOT (n)--()`) matches
nothing, so cleanups guarded by it silently keep every orphan; and an
`OPTIONAL MATCH (n)-[link]-() WITH n, link WHERE link IS NULL DELETE n`
pipeline returns the filtered row but does not apply the trailing `DELETE`
when the node previously had (since-deleted) relationships. A
`COUNT { (n)--() } = 0` predicate deletes the orphan correctly on both
v1.1.11 and the pinned Neo4j lane. The orphan-sweep subsystem still carries
the negated-pattern shape and is tracked separately.

Scope refinement (probed on v1.1.11 while fixing the rationale retract): the
zero-row behavior applies to a bare `MATCH` whose disjunction-labeled node is
filtered by a `WHERE` predicate, on either end of the pattern. A row-driven
`UNWIND $rows AS row MATCH (n:A|B|C {prop: row.value})` with an inline property
anchor DOES match and write correctly (the rationale EXPLAINS write template is
exactly this shape and creates every edge). Do not "fix" working UNWIND
inline-anchor writes; do fix every bare-MATCH disjunction retract or read.

Scope refinement (probed while backfilling the C-14 #4367 cloud-correlation
retracts): the managed-transaction-DELETE under-application is not limited to
multiple grouped statements. A **single** retract `DELETE` dispatched through
`ExecuteGroup` also under-applies on v1.1.11 — the same failure shape, just
with a group size of one. `KubernetesCorrelationEdgeWriter`,
`S3LogsToEdgeWriter`, `S3ExternalPrincipalGrantWriter`, and
`IAMInstanceProfileRoleEdgeWriter` each routed their single retract statement
through a shared `dispatch()` helper that used `ExecuteGroup` whenever the
executor implemented `GroupExecutor` — the shape `cmd/reducer` wires
unconditionally for every graph backend including NornicDB
(`reducerNeo4jExecutor.ExecuteGroup`). Fixed the same way as the per-label
retracts: each writer gained (or, for `SecurityGroupReachabilityWriter`,
reused) a `dispatchRetract` helper that always runs `Execute` sequentially,
never `ExecuteGroup`, live-proven in
`go/internal/storage/cypher/evidence-4367-cloud-edge-retract.md`. Do not
assume a single-statement group is safe from this class; the safe rule is
"retract DELETEs run through `Execute`, never `ExecuteGroup`," independent of
statement count.

That same backfill also resolved an open question about
`retractSecurityGroupSGRuleEdgesCypher`'s untyped relationship expansion
(`MATCH (sg:CloudResource)-[rel]->(rule:SecurityGroupRule) WHERE ... DELETE
rel`, anchoring on an unbound `[rel]` rather than a typed or disjunction
relationship pattern): probed directly over the HTTP `tx/commit` auto-commit
endpoint against a lean v1.1.11 container, seeding one
`CloudResource-[:ALLOWS_INGRESS]->SecurityGroupRule` edge and running the
exact retract statement as a single auto-commit statement deleted it (count 1
-> 0). The untyped-expansion shape itself is sound on this pinned version; it
was never the anchor pattern at fault, only the `ExecuteGroup` dispatch above.

### Validation

Run the static shape guard (no backend) and the backend-required retract proof
against the canonical v1.1.11 pin:

```bash
cd go
go test ./internal/storage/cypher -run TestCodeCallRetractStatementsUseSingleSourceLabel -count=1
ESHU_REPLAY_TIER_LIVE=1 bash ../scripts/verify-replay-tier.sh   # TestReducerCodeCallEdgeRetractGraphTruth, v1.1.11
```

No-Regression Evidence: the broken retract was a no-op (deleted nothing), so the
#5116 fix has no slower prior path to regress; the fix makes the intended scoped
retract work. `TestReducerCodeCallEdgeRetractGraphTruth` proves the in-scope
`CALLS`/`REFERENCES`/`INSTANTIATES` edges retract to zero while an out-of-scope
repo's edge and every endpoint node survive, on a real v1.1.11 NornicDB. The
per-label fan-out runs a bounded, fixed number of scoped deletes per retract.

No-Observability-Change: no runtime metric, span, log field, queue stage, worker
knob, or schema phase changes. The existing canonical retract spans and
graph-write failure/retry telemetry continue to expose retract behavior.

### Dispatch-Ordering Refinement: A Hoisted Retract Can Run Before Its Own Dependency (#5680)

Probed while fixing #5680: `nornicdb.PhaseGroupExecutor`'s mixed-phase
dispatcher (`executeGroupedChunksWithDrain`,
`go/internal/storage/nornicdb/phase_group_executor_retract.go`) is a
**Go-level dispatch-ordering bug**, distinct from the NornicDB query-shape
pitfalls this page otherwise documents. Before the fix, every Drain-marked
statement in a mixed phase (structural_edges, terraform_state) ran FIRST, as a
standalone autocommit statement, regardless of its position in the emitted
statement list; every remaining statement (Drain or not) then ran as one
`ge.ExecuteGroup` call, in its original relative order.

That hoisting silently broke the tfstate MATCHES_STATE edge retract
(`canonicalTerraformStateMatchesConfigEdgeRetractCypher`,
`tfstate_state_match_edge_retract.go`), which is Drain-marked and requires
`s.generation_id = $generation_id` — a property only refreshed by the resource
upsert statement `buildTerraformStateStatements` emits BEFORE it. Hoisted
ahead of that upsert, the retract's predicate matched zero rows every cycle: a
stale edge silently survived. Live-proven by
`TestTfstateMatchesStateEdgeRetractDispatchLive`
(`go/internal/replay/offlinetier/tfstate_dispatch_order_live_test.go`), which
fails against the pre-#5680 dispatcher and passes after the fix.

The fix makes `executeGroupedChunksWithDrain` order-preserving: it walks
statements in emitted order, flushing the pending non-retract group through
`ge.ExecuteGroup` immediately before every `OperationCanonicalRetract`
statement (Drain or not) and running that retract autocommit in its exact
position, mirroring `executeEntityPhaseGroup`'s own flush-then-autocommit
structure. This also means a retract is now NEVER dispatched through
`ge.ExecuteGroup` in this path (see `phase_group_executor_retract_dispatch_test.go`'s
`TestExecuteGroupedChunksWithDrainNeverDispatchesRetractViaExecuteGroup`),
matching this section's own "retract DELETEs run through `Execute`, never
`ExecuteGroup`" rule defensively for mixed-phase retracts, not only for the
dedicated per-writer `dispatchRetract` helpers this page otherwise describes.

Honest scope note: a live reproduction of "a non-Drain
`OperationCanonicalRetract` silently under-applies because it was bundled
into `ge.ExecuteGroup`" for the terraform_state resource-sweep DETACH DELETE
(`canonicalTerraformStateResourceRetractCurrentLabelCypher`) did NOT reproduce
in isolation against the pinned `timothyswt/nornicdb-cpu-bge:v1.1.11` image —
the old dispatcher already preserved relative order between non-Drain
statements (they were all appended to one "remaining" slice in emitted
order), so the resource-sweep retract still ran after its dependency upsert
committed within the same `ExecuteGroup` transaction, and the DELETE applied
correctly (`TestTfstateResourceRetractDispatchLive` passes on both the
pre-fix and post-fix dispatcher). The Drain-hoisting reorder above is the only
independently, live-confirmed root cause of #5680; the "never through
`ExecuteGroup`" change for non-Drain retracts is retained as a defensive
application of this page's established rule, not because a second live
failure mode was reproduced for this specific writer.

No-Regression Evidence: order-preserving dispatch flushes the pending
non-retract group at each `OperationCanonicalRetract` boundary instead of once
per phase, so a mixed phase issues a bounded number of extra `ExecuteGroup` /
autocommit segments — 2 per generation for `terraform_state`, at most one per
IaC family (Atlantis/Flux/GitLab/Helm ≤ 4) for `structural_edges` — scaled by
the count of retract statements in the phase, NOT by corpus size or entity
count. `executeGroupedChunksWithDrain` never used the concurrent chunk path
(that lives only in `executeEntityPhaseGroup`), the `PhaseGroupStatementLimit`
chunk cap is unchanged, and the live `structural_edges` edge-retract graph-truth
tests (`TestReducerCanonicalGovernanceEdgeRetractGraphTruth`,
`TestReducerCanonicalFluxReconcilesFromEdgeRetractGraphTruth`,
`TestReducerCanonicalFluxHelmReconcilesFromEdgeRetractGraphTruth`) pass
unchanged against the pinned v1.1.11 backend.

No-Observability-Change: the change is internal to
`executeGroupedChunksWithDrain`; it adds no metric, span, log field, queue
stage, or worker, and each dispatched segment rides the same existing statement
dispatch path.

## Pitfall: A Bare Top-Level UNION Returns Nothing When Its First Branch Is Empty

### Observed shape

Measured directly against the pinned NornicDB backend (`eshu-nornicdb-pr261`,
v1.1.11 base) via the Bolt HTTP `tx/commit` endpoint and independently via the
Go Neo4j driver over Bolt (both paths reproduce it):

```cypher
-- CloudResource has 0 matching nodes, TerraformVariable has 10:
MATCH (n:CloudResource) WHERE n.name CONTAINS 'cluster' RETURN n.id, n.name
UNION
MATCH (n:TerraformVariable) WHERE n.name CONTAINS 'cluster' RETURN n.id, n.name
-- returns 0 rows (broken) -- even though the second branch alone returns 10

-- Same two branches, order swapped (TerraformVariable now first):
MATCH (n:TerraformVariable) WHERE n.name CONTAINS 'cluster' RETURN n.id, n.name
UNION
MATCH (n:CloudResource) WHERE n.name CONTAINS 'cluster' RETURN n.id, n.name
-- returns the correct 10 rows

-- A THIRD branch with an empty match placed in the MIDDLE (not first) also
-- works correctly -- only an empty FIRST branch poisons the whole union.
```

`UNION ALL` reproduces the identical defect (not specific to `UNION`'s
deduplication pass). Wrapping the same union in a `CALL {...} RETURN ...`
subquery avoided it in every case tried, regardless of branch order or which
branch is empty:

```cypher
CALL {
  MATCH (n:CloudResource) WHERE n.name CONTAINS 'cluster' RETURN n.id as id, n.name as name
  UNION
  MATCH (n:TerraformVariable) WHERE n.name CONTAINS 'cluster' RETURN n.id as id, n.name as name
}
RETURN id, name
-- returns the correct 10 rows even with the empty branch first
```

### Eshu implications

Any handler that builds a per-label (or otherwise conditionally-empty-branch)
`UNION` chain directly at the top level of a Cypher statement can silently
return an empty result whenever its first branch happens to match nothing —
not an error, not a partial result, a *fully empty* result even though later
branches have real matches. This is easy to miss in testing: it only shows up
once cardinality is realistic enough that some candidate branches are empty
and others are not, which a small fixture with only one or two seeded rows
per label will not expose. Discovered fixing issue #5271's
`InfraHandler.searchResources` (`go/internal/query/infra.go`): its label list
starts with `CloudResource`, so any search against a corpus with zero
matching `CloudResource` nodes silently returned an empty result for every
other candidate label until the union was wrapped in `CALL {...}`.

Do not ship a top-level per-branch `UNION`/`UNION ALL` chain against this
backend without either wrapping it in `CALL {...} RETURN ...`, or proving at
realistic cardinality (not a two-or-three-row fixture) that every branch
ordering you can produce is exercised with at least one genuinely empty
branch in the first position.

### Validation

Reproduced via direct Bolt HTTP `tx/commit` calls against an isolated Compose
stack seeded with real Terraform fixture data (3,178 infra-labeled nodes
across multiple labels, one label with zero matches for the test query).
`go test ./internal/query -run TestSearchInfraResourcesWrapsUnionInCallSubquery
-count=1` is the static regression guard that the fix's `CALL {...}` wrapper
stays in place.

## Pitfall: Outer Aggregation Over A `CALL { ... }` Subquery Collapses The Group Key

### Observed shape

Measured directly against the pinned NornicDB backend (`eshu-nornicdb-pr261`,
v1.1.11 base) while fixing the infra resource aggregate reads (#5281). When a
per-label union is wrapped in a `CALL { ... }` subquery (the fix for the
empty-first-branch pitfall above) and the OUTER query re-aggregates the
subquery result, the non-aggregated group key evaluates to `null` and every row
collapses into a single bogus bucket:

```cypher
-- Per-label node collection, then group OUTSIDE the CALL: BROKEN
CALL {
  MATCH (n:CloudResource) RETURN n
  UNION ALL
  MATCH (n:TerraformResource) RETURN n
}
RETURN head(labels(n)) AS bucket, count(n) AS c   -- returns [(null, <grand total>)]

-- Per-branch group key returned as a column, re-aggregated OUTSIDE: also BROKEN
CALL {
  MATCH (n:CloudResource) RETURN head(labels(n)) AS bucket, count(n) AS c
  UNION ALL
  MATCH (n:TerraformResource) RETURN head(labels(n)) AS bucket, count(n) AS c
}
RETURN bucket, sum(c) AS c                          -- returns [(null, <grand total>)]
```

A scalar aggregation with no grouping key works (`CALL { ... } RETURN sum(c)`
returns the correct total), and a NON-aggregating outer passthrough of the
subquery columns works (`CALL { ... RETURN gexpr AS bucket, count(n) AS c }
RETURN bucket, c` returns the correct per-branch rows). Only outer aggregation
WITH a group key over the CALL result collapses.

### Eshu implications

Do not group or re-aggregate over a `CALL { ... }` subquery result on this
backend. Group inside each branch, pass the `(bucket, count)` rows through the
outer RETURN unchanged, and merge/sum the buckets application-side. The infra
resource aggregate reads (`go/internal/query/infra_resource_aggregates.go`,
`infra_resource_aggregates_cypher.go`) follow this shape: each per-label branch
returns its own grouped `(bucket, count)` rows, and Go sums them into the final
bucket map, then sorts and paginates. A bucket value can appear once per
contributing label, so the application-side step must SUM, not overwrite.

### Validation

Reproduced via direct Bolt HTTP `tx/commit` calls on the 91k-node
`eshu-5279-81` corpus: the old whole-graph `MATCH (n) WHERE (n:A OR ...) RETURN
head(labels(n)), count(n)` returned 16 correct buckets, both CALL-wrapped
outer-aggregation shapes above returned `[(null, 5653)]`, and the per-branch
passthrough + Go merge returned the same 16 buckets as the whole-graph read.
`go test ./internal/query -run TestInfraResourceScopePredicateRendersOnlyWhenScoped
-count=1` guards that the aggregate query stays per-label CALL-anchored.

## Pitfall: A `n:Label` Test Inside A CASE / Projection Returns Literal Text

### Observed shape

Measured directly against the pinned NornicDB backend (`eshu-nornicdb-pr261`,
v1.1.11 base) while fixing the infra all-categories by-provider grouping
(#5283). A node-label test used as a boolean **inside a projection or CASE**
is not evaluated — it is echoed as the literal expression text:

```cypher
MATCH (n:CloudResource {id:$id})
RETURN (n:CloudResource) AS istest,            -- returns the STRING "n:CloudResource"
       ('CloudResource' IN labels(n)) AS ok    -- returns the boolean true
```

Because a non-null string is truthy, a `CASE WHEN n:Label THEN a ELSE b END`
does not simply pick a branch — combined with nesting it corrupts the whole
expression. A deeply nested `CASE WHEN ... THEN CASE WHEN n:CloudResource THEN
CASE WHEN ... END END ELSE ... END` group key collapsed every row (including
rows whose top-level branch never reached the label test) to a `null` bucket:

```cypher
-- BROKEN group key: nested CASE-in-CASE with a n:Label test
CASE WHEN n.provider IS NULL OR n.provider = ''
     THEN CASE WHEN n:CloudResource
               THEN CASE WHEN n.source_system IS NULL OR n.source_system = ''
                         THEN 'unknown' ELSE n.source_system END
               ELSE 'unknown' END
     ELSE n.provider END
-- => every provider-less node buckets to null; even n.provider='gcp' mis-buckets
```

A label test in a **WHERE clause** (`... WHERE n:Label AND ...`) IS evaluated as
a boolean and works correctly; only the projection/CASE position is defective.

### Eshu implications

Never gate on a label with a `n:Label` test inside a RETURN projection or CASE
on this backend. Use `'Label' IN labels(n)` — `labels(n)` is evaluated correctly
— and keep the CASE flat (single level of `WHEN` branches) rather than nesting
CASE inside CASE. The infra by-provider grouping
(`infraResourceProviderGroupExpression`,
`go/internal/query/infra_resource_aggregates.go`) now emits a flat
`CASE WHEN coalesce(n.provider,'') <> '' THEN n.provider WHEN ('CloudResource'
IN labels(n)) AND coalesce(n.source_system,'') <> '' THEN n.source_system ELSE
'unknown' END`. WHERE-clause label tests in the same file
(`infraResourceAggregateFilterClauses`) are left as-is because the WHERE
position evaluates them correctly.

### Validation

Reproduced live via the Bolt driver on an isolated pinned NornicDB with a
4-node seed across `CloudResource` and `TerraformResource`: the old nested
label-test group key returned `{"":3, "unknown":1}` (null bucket, `gcp` lost);
the flat `IN labels(n)` form returned the intended `{aws:1, gcp:1, unknown:2}`.
`go test ./internal/query -run
TestInfraResourceInventoryGroupExpressionsAreNornicDBSafe -count=1` guards that
no inventory group expression reintroduces a `n:Label` test in a projection, and
`TestLiveInfraProviderInventoryBucketsNonNull`
(`ESHU_INFRA_AGG_PROVE_LIVE=1`) is the backend-required live proof.

## Pitfall: An Empty-String-Guarded `OR` Disjunct Collapses The Whole Predicate

### Observed shape

Measured against the pinned NornicDB backend (`eshu-nornicdb-pr261`, v1.1.11
base) while fixing the resource-investigation impact reads (#5287). A predicate
that guards an optional-parameter disjunct with an empty-string test returns
**zero rows** even when the primary disjunct matches, when the guarded parameter
is empty:

```cypher
-- BROKEN when $resource_arn = '':
MATCH (instance:WorkloadInstance)-[rel:USES]->(resource)
WHERE coalesce(resource.id, resource.uid, resource.resource_id, resource.name) = $resource_id
   OR ($resource_arn <> '' AND resource.arn = $resource_arn)
RETURN ...
-- => 0 rows, even though the coalesce(...) = $resource_id disjunct is true.
```

Isolated on a live seed: `coalesce(...) = $resource_id` alone returns the row;
adding `OR ($resource_arn <> '' AND resource.arn = $resource_arn)` with
`$resource_arn = ''` drops it to zero rows; `coalesce(...) = $resource_id OR
resource.arn = $resource_arn` (no `<> ''` guard, with a non-matching arn) returns
the row. The `'' <> ''` guard sub-expression mis-evaluates and poisons the
enclosing `OR`.

### Eshu implications

Do not gate an optional disjunct with `$param <> '' AND ...` on this backend.
Build the predicate conditionally in Go: emit only the primary disjunct when the
optional value is empty, and append ` OR n.prop = $param` (without the guard)
only when the value is present, binding `$param` in lockstep. The
resource-investigation anchor (`resourceInvestigationResourceAnchor` /
`resourceInvestigationAnchorParams` in
`go/internal/query/impact_resource_investigation.go`) follows this shape.

### Validation

Reproduced live via the Bolt driver on an isolated pinned NornicDB. `go test
./internal/query -run TestResourceInvestigationResourceAnchorOmitsEmptyArnGuard
-count=1` guards that the anchor never reintroduces the `<> ''` guard, and
`TestLiveResourceInvestigationReadsAreNornicDBSafe`
(`ESHU_INFRA_AGG_PROVE_LIVE=1`) is the backend-required live proof.

## Pitfall: A Variable-Length Path With A Fresh Far End Needs A Labelled Start

### Observed shape

Measured against the pinned NornicDB backend (`eshu-nornicdb-pr261`, v1.1.11
base) while fixing the route-to-caller reads (#5287). A single-clause
variable-length path whose anchored endpoint is unlabelled and whose far end is a
fresh variable returns zero rows:

```cypher
-- BROKEN: unlabelled anchored end, fresh far end -> 0 rows
MATCH path = (caller)-[:CALLS*1..5]->(handler) WHERE handler.id = $hid
RETURN nodes(path) AS chain
-- BROKEN: unlabelled anchored start -> 0 rows
MATCH path = (handler)<-[:CALLS*1..5]-(caller) WHERE coalesce(handler.id, handler.uid) = $hid
RETURN nodes(path) AS chain

-- WORKS: the anchored node carries a label
MATCH path = (handler:Function)-[:CALLS*1..5]->(callee) WHERE handler.id = $hid
RETURN nodes(path) AS chain
```

A path whose BOTH endpoints are pre-bound in their own `MATCH` clauses works
without a label on the path pattern (e.g. `buildNornicDBCallChainCypher`'s
`MATCH (start {uid:$s}) MATCH (end {uid:$e}) MATCH path=shortestPath((start)-[:CALLS*1..N]->(end))`),
because the endpoints are already bound nodes. Only a fresh-variable far end
needs the anchored end labelled.

### Eshu implications

When traversing from a known node to discover unknown neighbours over a
variable-length relationship, anchor the known node with a label. If the label is
not known statically, resolve it first (`RETURN head(labels(n))`) and interpolate
it — gated against a whitelist so the label is never attacker-influenced. The
route-to-caller relationship reads (`routeToCallerHandlerLabel` +
`routeToCallerDirectionRows` in `go/internal/query/code_route_to_caller_graph.go`)
resolve the handler label, then anchor `(handler:<Label>)` as the path start and
project raw `nodes(path)`.

## Pitfall: Node-Identity Inequality `a <> b` Returns Zero Rows

### Observed shape

Measured on the pinned NornicDB backend (#5287). Comparing two whole nodes with
`<>` in a WHERE clause drops all rows:

```cypher
-- BROKEN: node-identity inequality -> 0 rows
MATCH path = (handler:Function)-[:CALLS*1..5]->(callee) WHERE handler.id = $hid AND callee <> handler
RETURN nodes(path) AS chain

-- WORKS: compare identity properties instead
MATCH path = (handler:Function)-[:CALLS*1..5]->(callee)
WHERE handler.id = $hid AND coalesce(callee.id, callee.uid) <> coalesce(handler.id, handler.uid)
RETURN nodes(path) AS chain
```

### Eshu implications

Never express "these two nodes are different" as `a <> b` on this backend.
Compare stable identity properties: `coalesce(a.id, a.uid) <> coalesce(b.id,
b.uid)`. The route-to-caller directional reads use this form to exclude the
handler itself from its own caller/callee set.

## Pitfall: Multi-Clause Read Queries Silently Corrupt The Projection

### Observed shape

On the pinned build a read query that places any clause (`WITH`, a second
`MATCH`, or `OPTIONAL MATCH`) between the anchoring `MATCH` and the final
`RETURN` routes into a string-slicing interpreter that cannot faithfully
evaluate the projection. Measured directly over Bolt-HTTP against a seeded
`Repository`/`DEPENDS_ON` graph:

```cypher
-- RETURN DISTINCT <expr> AS alias after a preceding clause -> LITERAL TEXT:
MATCH (s:Repository) WHERE s.name CONTAINS $t
OPTIONAL MATCH path=(s)<-[:DEPENDS_ON*1..5]-(a:Repository)
RETURN DISTINCT a.name AS repo          -- value comes back as "DISTINCT a.name"

-- length(path) once another clause follows the path-declaring clause -> 0 / literal:
MATCH (s:Repository) WHERE s.name CONTAINS $t
OPTIONAL MATCH path=(s)<-[:DEPENDS_ON*1..5]-(a:Repository)
RETURN a.name AS repo, length(path) AS hops   -- hops = 0

-- min()/max()/aggregate in a multi-clause projection -> literal "min(length(path))"
-- WITH ... (aggregating or bare) then OPTIONAL MATCH -> zero rows (row-drop)
-- a MAP-valued comprehension with rel-property access -> mangled:
RETURN [rel IN relationships(path) | {c: rel.confidence}]  -- value leaks "map[…].confidence"
-- a trailing OPTIONAL MATCH after CALL {} -> 500 "unsupported clause after CALL {}"
```

Untyped variable-length `[*1..N] WHERE all(rel IN rels WHERE type(rel)='X')`
separately matches zero rows; typed `[:X*1..N]` works. Zero-length `*0..N`
projects literal text for the hop-0 row.

### Eshu implications

`impact/blast-radius` served literal alias strings (`"DISTINCT affected.name"`)
and `affected_count:1` for a 19-repo blast radius, and its `sql_table` branch
hard-errored — issue #5279. The safe contract is a **single anchoring clause**:
one `MATCH … WHERE … RETURN` with the aggregate (`min(length(path))`) and
`ORDER BY`/`LIMIT` in that same clause, typed var-length traversal, plain value
or SCALAR-comprehension projection (`[rel IN relationships(path) | type(rel)]`,
`[n IN nodes(path) | n.id]` evaluate correctly; map-valued comprehensions with
relationship-property access do not), `CALL{…UNION…}` with a plain outer
`RETURN` for multi-branch reads, and any secondary join (tier lookup) run as a
SEPARATE single-clause query merged in Go. See
`go/internal/query/impact_blast_radius.go` and the guard test
`TestBlastRadiusQueriesAreNornicDBSafe`. These shapes are also strictly more
correct on Neo4j (`RETURN DISTINCT repo, hops` double-counts diamond-reachable
repos and inflates `LIMIT`).

This corruption compounds with the node-label-disjunction pitfall above: an
impact read that anchors on `MATCH (n:A|B|C) WHERE n.id = $id` (e.g.
`trace-resource-to-code`, `explain-dependency-path` via
`impactAnchorLabelDisjunction`) is broken by BOTH bugs and returns zero rows;
the safe fix is a per-label inline-property anchor (one `MATCH (n:Label {id:$id})`
per label) plus the single-clause projection contract.

Both by-id impact reads were fixed this way (#5286,
`go/internal/query/impact_anchor_resolve.go`, `impact.go`, guard test
`TestImpactAnchorResolveCypherIsPerLabelUnion`, live proof
`docs/internal/evidence/5286-by-id-impact-anchors-nornicdb.md`):
`trace-resource-to-code` folds the label resolution into the traversal as a
`CALL{UNION}` of per-label inline-property anchors (one round-trip), and
`explain-dependency-path` resolves both endpoints' labels in one `CALL{UNION}`
then runs `shortestPath` with single-label inline-property anchors on both ends.
Hop provenance is unwound from the raw `relationships(path)`/`nodes(path)` lists
in Go (the map-valued rel-property comprehension mangles). `nodes(path)`
deserializes as `neo4j.Node` on both backends; `relationships(path)` as
`neo4j.Relationship` on Neo4j and a `map[string]any` on NornicDB.

The property-join variant was fixed the same way for the `trace-deployment-chain`
OCI registry-truth reads (#5287): the old two-MATCH digest read returned a null
`coalesce(image.id, image.descriptor_id)` and the old three-MATCH tag read
dropped every row, both replaced with single-clause per-label reads joined in Go
(`go/internal/query/impact_trace_deployment_oci.go`, guard test
`TestOCIRegistryTruthQueriesAreNornicDBSafe`, live proof
`docs/internal/evidence/5287-trace-deployment-oci-nornicdb.md`).

The change-surface variable-length impact traversals (`/change-surface`,
`/change-surface/investigate`) were fixed the same way (#5287): the investigate
read (two MATCH + `RETURN DISTINCT` + `length(path)`) returned zero rows and the
legacy read (`OPTIONAL MATCH` + `UNWIND relationships(path)` + `WITH` + `RETURN
DISTINCT`) returned a single all-null row. Both collapse to a single
`MATCH path = (start:Label {id:$id})-[*1..N]->(impacted)` clause that folds the
anchor into the path pattern, with `min(length(path))` for the investigate read
and a raw `relationships(path)` projection unwound per-edge in Go for the legacy
read (`go/internal/query/impact_change_surface_response.go`,
`impact_change_surface_legacy.go`, guard test
`TestChangeSurfaceTraversalQueriesAreNornicDBSafe`, live proof
`docs/internal/evidence/5287-change-surface-nornicdb.md`). Two supporting
findings from that proof:

- A **bare untyped** variable-length traversal `(start)-[*1..N]->(impacted)`
  DOES traverse correctly in a single clause (unlike the untyped `[*1..N] WHERE
  all(type(rel)=…)` retract shape above, which matches zero rows). Only the
  surrounding multi-clause shape corrupted the old reads.
- A `WHERE` predicate of the form `($param = '' OR coalesce(n.prop, '') = '' OR
  n.prop = $param)` silently drops **every** row when combined with a
  `relationships(path)` projection on the pinned build — the `$param = ''`
  parameter-comparison disjunct is the offender. The narrower node-only form
  `(n.prop = $param OR coalesce(n.prop, '') = '')` is safe with the same
  projection (drop the empty-parameter disjunct and only add the predicate when
  the parameter is set).

### Validation

`go test ./internal/query -run 'TestBlastRadius|TestFindBlastRadius|TestMergeBlastRadius' -count=1`
(the query-shape guard fails on the pre-#5279 multi-clause shapes) plus the live
Bolt-HTTP before/after in `docs/internal/evidence/5279-blast-radius-nornicdb.md`.

### Upstream fix (pinned until merged into NornicDB main)

A fail-loud guard for these shapes is proposed upstream at
[orneryd/NornicDB#263](https://github.com/orneryd/NornicDB/pull/263) (branch
`fix/fail-loud-multiclause`, base commit `1492458852588c884c32f70d27ea2ee07086769c`):
the executor errors instead of returning corrupt rows. It is **not yet in the
pinned image** and does not change Eshu correctness on its own (reads are being
rewritten single-clause-safe regardless — #5279, #5287); do not image-pin that
branch before the Eshu sweep completes. Pinned to that PR/branch until it lands
in NornicDB `main`.
