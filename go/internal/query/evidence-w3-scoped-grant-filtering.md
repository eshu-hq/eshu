<!--
SPDX-License-Identifier: MIT
Copyright (c) 2025-2026 eshu-hq
-->

# F-6 W3 scoped-token grant filtering — performance & observability evidence

This note is the tracked performance/observability evidence for the F-6 W3
change set (#5167, parent #5161): five stacked grant-binding fixes that close
cross-tenant repository disclosure on the impact / service / repository read
route family. It exists so a future agent can see, in-repo, why binding the
caller's grant on these read paths carries no query-plan regression.

## What changed

Each fix adds an **output-preserving-shape Go post-processing filter** over an
already-materialized, already-bounded result set. None of them changes any
Cypher string, adds a graph scan, or alters an existing `LIMIT`:

- `filterRepositoryArtifactSourcesForAccess` (repository_config_artifacts_loader.go)
  — deployment-artifact-overview fallback related-repo sources.
- `filterRepoRelationshipTargetRowsForAccess` /
  `filterRepoRelationshipOverviewRowsForAccess` (repository_context_helpers.go)
  — `queryRepoDependencies` / `queryRepoConsumers` / `queryRepoRelationshipOverview`.
- `filterRepositoryRelationshipReadModelForAccess` (repository_relationship_read_model.go)
  — the Postgres `resolved_relationships` read-model rows + consumers.
- `filterProvisioningRepositoryCandidatesForAccess` (impact_access_filter.go)
  — provisioning candidates feeding dependents / consumer_repositories /
  provisioning_source_chains.
- `filterDeploymentEvidenceRowsForAccess` / `redactDeploymentEvidenceRowForAccess`
  (repository_deployment_evidence.go) — the pre-existing EvidenceArtifact choke
  point (base F-6 W3 commits).

All reuse the shared `impactRepoIDAllowed` / `repositoryAccessFilter`
grant-membership predicate.

No-Regression Evidence:

- Change class: output-preserving-shape optimization-neutral post-filter. The
  Cypher (and the Postgres read-model SQL) is byte-for-byte UNCHANGED. This is
  proven by the honest `source_sha256` digest refreshes in
  `go/internal/queryplan/grandfathered_non_hot.go` for
  `queryRelatedRepositoryArtifactSources`, `queryRepoDependencies`,
  `queryRepoConsumers`, and `(*RepositoryHandler).getRepositoryContext`: the
  query-plan gate's source digest changed ONLY because the enclosing Go function
  body gained a post-query filter call, not because the query text changed. The
  query-plan-regression gate itself passes on this diff (the Cypher entries are
  unchanged), confirming no new unlabeled MATCH, no unbounded variable-length
  traversal, and no plan shift.
- Baseline (OLD shape): the pre-fix handler runs the exact same query and
  returns ALL rows to the caller (the leak).
- After (NEW shape): the identical query result is passed through an O(rows) Go
  grant filter before it reaches the response. Terminal row count is a STRICT
  SUBSET of the baseline: filtered ≤ unfiltered. A grant filter can only DROP
  unauthorized rows, so it can only reduce serialized payload and latency, never
  add graph or Postgres work.
- Backend / version: NornicDB pinned image (eshu-nornicdb PR-261 branch, the
  repo's docker-compose graph backend pin); Postgres for the read-model path.
  No backend knob, worker count, batch size, lease, or concurrency setting is
  touched by this change.
- Input shape: the impact / service / repository route result sets, already
  bounded by the existing limits on these paths
  (`repositoryDeploymentEvidenceArtifactLimit = 50`, `serviceStoryItemLimit`,
  and the bounded trace-enrichment limit). The filters iterate that bounded
  set once; they never widen a scan or issue a new query.
- Terminal row counts: filtered ≤ unfiltered (strict subset), proven per route
  by the revert-to-RED regression tests
  (`auth_scoped_routes_impact_evidence_fallback_test.go`,
  `auth_scoped_routes_relationship_leak_test.go`,
  `repository_context_relationship_read_model_leak_test.go`,
  `service_query_enrichment_provisioning_leak_test.go`): scoped callers get the
  cross-tenant rows dropped, all-scope callers get the unchanged full set.

Benchmark Evidence:

Micro-benchmark of the filter helpers at the representative worst-case row
count (50, the bounded LIMIT on these paths), granting half the repos so both
keep and drop branches run. Machine: Apple M1 Max (darwin/arm64), Go test
`-benchmem`, in `go/internal/query/w3_scoped_grant_filter_bench_test.go`:

```
BenchmarkFilterRowsByRepoIDForAccess-10                    1220 ns/op    416 B/op   1 allocs/op
BenchmarkFilterProvisioningRepositoryCandidatesForAccess-10 1692 ns/op  4096 B/op   1 allocs/op
BenchmarkFilterRepoRelationshipOverviewRowsForAccess-10    1921 ns/op    416 B/op   1 allocs/op
BenchmarkImpactRepoIDAllowed-10                            9.98 ns/op      0 B/op   0 allocs/op
```

The whole-slice filters cost ~1.2–1.9 µs for a 50-row set (one allocation for
the result slice); the per-row grant check is ~10 ns with zero allocations.
This is three-to-four orders of magnitude below a single graph/Postgres round
trip on these routes (milliseconds), so the added Go work is not measurable in
end-to-end route latency, and the strictly smaller serialized payload can only
help.

Observability Evidence:

No new spans, metrics, or structured-log fields were added, and no span
timing/attribution semantics changed. One honest value update: the
`graph_provisioning_candidates` stage's existing `row_count` span attribute in
`service_query_enrichment.go` is now recorded AFTER the grant filter, so it
reports the count actually returned to the caller (the filtered count) rather
than the raw pre-filter count. This is the correct, honest reading of that
stage's "rows returned" attribute and matches how the other stages already log
their post-processing counts; it changes no span name, timing boundary, or
error signal. Operators lose no visibility — the stage timer still wraps the
same work, and a scoped caller seeing fewer candidate rows is the intended,
observable behavior. No dashboard, alert, or status surface depends on the
pre-filter count.

## Filter-before-limit fix (#5167 W3 P1)

The five fixes above filter the caller's grant AFTER the query. On four
paginated routes that is not sufficient: the query's own `LIMIT` runs before the
Go filter, so when more than `limit` cross-tenant rows sort ahead of a granted
row, the granted row falls off the page and the post-fetch filter returns an
incomplete (sometimes empty) page with `truncated:false`. The affected routes:

- `impact/blast-radius` — the five affected-repo Cypher consts in
  `impact_blast_radius.go` (repository, terraform source/dependents, crossplane,
  sql_table CALL{} branches).
- `impact/change-surface` investigation and `impact/resource-investigation`
  resolvers — the grant is threaded into the resolver Cypher WHERE.
- `impact/change-surface` code-search (`impact_change_surface_code.go`) — a
  corpus-wide scoped search binds `repo_id = ANY($n)` (pq.Array) into the
  Postgres WHERE before `ORDER BY / LIMIT / OFFSET`.

This is a **behavior change, not output-preserving**: the pre-fix scoped page
could OMIT a row the caller is entitled to. The corrected page contains the
granted row the old shape dropped.

No-Observability-Change:

The grant push-down renders inside the existing blast-radius / change-surface /
resource-investigation graph reads and the code-search Postgres read. It adds no
new span, metric, runtime knob, queue behavior, or graph write; the handlers
keep their existing `GraphQuery.Run` / content-store adapters and per-query
duration/error telemetry. `filterRowsByRepoIDForAccess` stays as
defense-in-depth after the query, so a non-scoped caller's query is
byte-identical to the pre-#5167 shape (the grant fragment is empty) and the page
is unchanged.

No-Regression Evidence:

- Change class: correctness fix on the scoped read path; the grant fragment is a
  bound-node property predicate (`a.id IN $allowed_repository_ids OR
  a.id IN $allowed_scope_ids`) injected into an existing single-clause /
  CALL{UNION}-plain-outer WHERE. It adds NO relationship traversal — proven by
  `TestBlastRadiusGrantFragmentAddsNoEdgeTokens`
  (`extractRelationshipTypeTokens` returns empty for every grant fragment), so
  the #5335 edge-materialization gate and the NornicDB-safe shape contract
  (`TestBlastRadiusQueriesAreNornicDBSafe`) both still hold for the scoped
  variant.
- Non-scoped callers: the grant fragment is `""` when `!access.scoped()`, so the
  blast-radius consts render byte-identical to their pre-#5167 text and the
  query plan is unchanged. The blast-radius consts stay literal `BasicLit`
  templates so the #5335 gate can AST-parse each one
  (`TestImpactBlastRadiusGateQueriesAreLiteralConstants`).
- Bounded input: the grant predicate only ADDS a WHERE conjunct on an
  already-anchored, already-`LIMIT`-bounded match. It filters the granted set
  before the same `LIMIT`, so terminal row count is filtered ≤ unfiltered and
  the graph/Postgres work can only shrink (the backend evaluates the extra
  property predicate on rows it already visits for the anchor match).
- Regression proof: `TestBlastRadiusGrantBoundBeforeLimit` drives the production
  `blastRadiusAffected` builder against an honest in-memory graph that applies
  the grant iff the Cypher carries the `IN $allowed_repository_ids` marker, sorts
  per `ORDER BY hops, repo`, then truncates to `limit`. Its RED companion runs
  the grant-free (pre-fix) shape through the same fake and asserts the granted
  row is LOST after truncation; the GREEN assertion proves the shipped query
  keeps it. The `queryplan` `source_sha256` for `blastRadiusAffected` was
  refreshed in `query-source-coverage.yaml` because the enclosing Go function
  now threads `access` into the builders; the two resolver entries
  (`resolveChangeSurfaceTarget`, `resolveResourceInvestigationTarget`) moved from
  grandfathered prose to typed `non_hot` dispositions.

## Why the change is safe

- Tenant isolation: each filter binds the FAR repository endpoint (the one that
  is NOT the grant-verified anchor) to the caller's grant, closing the
  cross-tenant repository-identity disclosure the F-6 W3 review found on the
  impact / service / repository route family (graph AND Postgres read-model
  dual paths, plus provisioning candidates).
- Deny-by-default when scoped: an empty or out-of-grant related repo id is
  dropped; the anchor-aware relationship filter keeps the grant-verified anchor
  endpoint and requires the other endpoint to be in grant. Same pure
  grant-membership semantics as the existing `filterRowsByRepoIDForAccess` —
  no new public-vs-private carve-out is invented.
- Non-scoped callers unaffected: every filter short-circuits and returns its
  input unchanged when `!access.scoped()` (all-scopes, shared-key, admin, or
  unauthenticated local), so shared/operator read behavior is byte-identical to
  before. The all-scope control assertions in the four regression tests prove
  the rows still flow for non-scoped callers.
