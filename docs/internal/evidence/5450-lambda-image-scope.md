# #5450 P1-B / P1-C — AWS_lambda_function_uses_image relationship read fixes

PR #5751 review (codex) surfaced three P1 findings on the #5450 edge exposure.
Two were real read-path defects; one was not.

## P1-A — anchor by uid — NOT a bug

Claim: `getRelationships`' `MATCH (n) WHERE n.id = $entity_id` cannot find the
Lambda `CloudResource` because the AWS materializer keys it by `uid`, not `id`.
Disproved against source: `go/internal/storage/cypher/cloud_resource_node_writer.go`
does `MERGE (r:CloudResource {uid: row.uid}) SET r.id = row.uid`, so every
`CloudResource` (including the Lambda rows written through the same
`WriteCloudResourceNodes` path) mirrors `uid` into `id`. The `n.id` anchor
resolves the node. No code change; thread resolved with this explanation.

## P1-B — mixed-case verb never resolves — REAL

`relationshipVerbByName` was keyed by the raw `entry.verb`, but every lookup
site (`getRelationshipEdges`, `relationshipEvidenceTargetAttributable`)
upper-cases the requested verb first. `AWS_lambda_function_uses_image` is the
only catalog verb whose canonical form is not already all-uppercase, so its
lookup key never matched and every casing returned `400 unknown relationship
verb`.

Fix: key `relationshipVerbByName` by `strings.ToUpper(entry.verb)`;
`entry.verb` stays mixed-case (it is the literal graph relationship-type token
and the API's echoed `verb` field).

Proof — `go test ./internal/query -run TestGetRelationshipEdgesResolvesMixedCaseVerb`:
both `AWS_lambda_function_uses_image` and `AWS_LAMBDA_FUNCTION_USES_IMAGE` now
return `200` with `verb == "AWS_lambda_function_uses_image"`; before the fix
both returned `400`.

## P1-C — scoped caller sees zero Lambda edges — REAL

The Lambda edge's authoritative reducer scope is stamped on the relationship
(`rel.scope_id`, `cloud_resource_container_image_edge_writer.go`), and the
`CloudResource` source node carries no `repo_id`/`scope_id`. The scoped read
predicate bound only the source node (`infraResourceScopeCoreDisjuncts` on `s`),
so a legitimately scoped caller matched nothing and saw zero edges.

Fix: for the `edgeScopeAttributable` verb, fold `r.scope_id IN
$allowed_scope_ids` into the SAME flat source-endpoint OR-group as its final
disjunct — one atomically parenthesized chain
`(s.repo_id ... OR ... OR r.scope_id IN $allowed_scope_ids)`. This keeps the
shape identical to the endpoint predicate every other scoped verb already ships
(one disjunct wider), AND-combines safely after the `source_tool` filter in the
filtered variant, and uses only a flat property `IN` compare (no NornicDB
label-disjunction or `EXISTS`-subquery traps). The verb is asserted
endpoint-source-only (`edgeScopeAttributable` implies `!targetAttributable`,
`TestRelationshipVerbCatalogEdgeScopeInvariant`).

Failing-then-green proof against a live NornicDB (`nornicdb-cpu-bge:v1.1.11`,
Bolt) — `TestRelationshipEdgesScopedCallerSeesLambdaImageEdgeLive`, seeding a
real `CloudResource {uid, id}` Lambda node + `ContainerImage` node and a
`MERGE (source)-[rel:AWS_lambda_function_uses_image {scope_id}]->(target)` edge:

- edge-scope fold DISABLED (pre-fix predicate): a caller granted the exact
  edge `scope_id`, holding no repository grant, gets **0 edges**
  (`want exactly 1 ... got 0`) — the bug reproduces.
- edge-scope fold ENABLED (this change): the same caller gets exactly **1 edge**
  with the seeded endpoints, and a caller granted a DIFFERENT scope gets
  **0 edges** (no cross-tenant widening).

Every other catalog verb's scoped Cypher stays byte-identical (no `r.scope_id`
reference), asserted by `TestRelationshipEdgesScopeBindsEdgeScopeForLambdaImageVerb`.

## Markers

No-Regression Evidence: this is a query-read correctness fix, not a rewrite of a
proven-correct path. The scoped Cypher for every non-`AWS_lambda_function_uses_image`
verb is unchanged (byte-identical WHERE), proven by
`TestRelationshipEdgesScopeBindsEdgeScopeForLambdaImageVerb`; the changed verb
gains one flat OR-group disjunct (`r.scope_id IN $allowed_scope_ids`) that keeps
the same index-anchored `MATCH (s:CloudResource)-[r:AWS_lambda_function_uses_image]->(t)`
shape and returns via the existing `LIMIT $limit` short-circuit. Backend: live
NornicDB `nornicdb-cpu-bge:v1.1.11` over the Bolt driver. Input shape: one
seeded Lambda `CloudResource`/`ContainerImage`/edge with `rel.scope_id`. Before:
scoped caller `0` edges (wrong); after: `1` edge for the granted scope, `0` for
a different scope. `go test ./internal/query -count=1` green (no regression) and
`go vet ./internal/query` clean.

No-Observability-Change: no metric, span, or log is added or removed. The
relationships edge endpoint's existing request/latency instrumentation covers
the read unchanged; the fix only widens one verb's scope WHERE-clause and
re-keys an in-memory verb index.

## #5738 — MCP `query_type` enum never advertised the edge — REAL

P1-B (above) fixed the raw canonical-value lookup
(`AWS_lambda_function_uses_image` / `AWS_LAMBDA_FUNCTION_USES_IMAGE` both
resolve), and #5751 added the verb to `relationshipVerbCatalog`. Neither
touched `infraRelationshipTypeAliases` or the `analyze_infra_relationships`
MCP tool's `query_type` JSON-schema `enum`
(`go/internal/mcp/tools_ecosystem.go`), which only ever listed the five
semantic aliases (`what_deploys`, `what_provisions`, `who_consumes_xrd`,
`module_consumers`, `what_runs_image`) — never a raw edge-type string. An MCP
client (the tool-calling model) only ever sees the advertised enum, so it had
no declared `query_type` value that reaches this edge; the raw canonical-value
workaround only helped a caller of the HTTP route who already knew the exact
graph relationship-type string.

Fix: added a `what_runs_lambda_image` alias to `infraRelationshipTypeAliases`
(`infra_relationship_filter.go`) resolving to `AWS_lambda_function_uses_image`
with the stored case preserved (same map-value pattern P1-B established), and
added `what_runs_lambda_image` to the MCP tool's `query_type` enum plus the
OpenAPI `relationship_type` enum (`openapi_paths_infrastructure.go`) — the
same three-surface shape `what_runs_image` (#5436) used for the analogous
`RUNS_IMAGE` gap.

Failing-then-green proof —
`go test ./internal/query -run 'TestInfraRelationshipsWhatRunsLambdaImage|TestResolveInfraRelationshipTypes' -v -count=1`:
before the alias, `TestInfraRelationshipsWhatRunsLambdaImageSurfacesOutgoingEdge`
got `400 unknown relationship_type: what_runs_lambda_image` and the
`what_runs_lambda_image_alias` table case in `TestResolveInfraRelationshipTypes`
got `ok = false`; after, both pass with a `200` response bounding the Cypher to
`:AWS_lambda_function_uses_image`. The reverse-direction
`TestInfraRelationshipsWhatRunsLambdaImageSurfacesIncomingEdge` (anchored on
the `ContainerImage`, using the already-working raw canonical value) passed
before and after, proving the bidirectional `getRelationships` pattern was
never the gap — only the semantic alias was missing.

Golden corpus (B-7/B-12): verified non-vacuous against the actual corpus
fixtures before adding the assertion, not by assumption. The awscloud
`supply-chain-demo` cassette's `aws_resource` fact for
`arn:aws:lambda:us-east-1:123456789012:function:supply-chain-demo`
(`resource_type: lambda.function`) and its paired `aws_relationship`
(`lambda_function_uses_image`, `resolved_image_uri` ending
`@sha256:...cc`) are joined by `DomainAWSCloudImageMaterialization`
(`aws_cloud_image_join.go`) against the `ociregistry` `supply-chain-demo`
cassette's ECR `oci_registry.image_manifest` fact for the same digest
(`descriptor_id: oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo@sha256:...cc`)
— the same materialization rc-166 already golden-asserts at the graph level.
The source `CloudResource` uid and the target `ContainerImage` uid used in the
new `testdata/golden/e2e-20repo-snapshot.json` HTTP read-path assertion
(`POST /api/v0/infra/relationships?query_type=what_runs_lambda_image`) were
computed by running the actual production code paths
(`ExtractCloudResourceNodeRows` / `cloudResourceUID` for the source,
`containerImageNodeUIDFromDigestRef` for the target) against the exact cassette
payloads in a throwaway same-package test, not hand-derived — both matched the
independently-observed cassette `descriptor_id` for the target, cross-checking
the computation. `go test ./internal/goldengate/... ./cmd/golden-corpus-gate/...
-count=1` (static snapshot parsing/schema) passed against the edited file; the
live gate itself is orchestrator-run, not run from this change.

No-Regression Evidence: this only adds a map entry to
`infraRelationshipTypeAliases` and enum entries to the MCP tool schema and
OpenAPI; the Cypher-building functions (`infraRelationshipTypeClause`,
`infraRelationshipAnchorClause`, `infraRelationshipNeighborClause`) and every
other alias/canonical resolution are untouched, proven by
`TestResolveInfraRelationshipTypes` (all prior cases still pass) and
`TestInfraRelationshipsHonorsRelationshipTypeFilter`/
`TestInfraRelationshipsAcceptsCanonicalEdgeType`/
`TestInfraRelationshipsAcceptsMixedCaseCanonicalEdgeType` (unchanged, still
green). `go test ./internal/query/... -count=1` and
`go test ./internal/mcp/... -count=1` both green; `go build ./...` and
`go vet ./internal/query/... ./internal/mcp/...` clean;
`bash scripts/verify-openapi.sh` reports the route surface clean
(253 HandleFunc routes / 253 OpenAPI path entries, unchanged — this PR adds an
enum value, not a route).

No-Observability-Change: no metric, span, or log is added, removed, or
relabeled. `getRelationships`' existing per-request span
(`telemetry.SpanQueryInfraRelationships`) and its
`eshu.relationship_filter` attribute already cover every resolved alias
generically (`infraRelationshipFilterLabel`); adding one more alias to the map
it reads from needs no new instrumentation.
