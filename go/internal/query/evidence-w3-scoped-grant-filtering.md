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
