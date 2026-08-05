# internal/query evidence and change history (part 2)

Part 2 of 3 of the dated, per-issue evidence and rationale entries for
`go/internal/query`, split out of [AGENTS.md](AGENTS.md) to keep the
read-first guidance under the repository's 500-line cap. Read the entry for a
specific issue number before touching the routes, helpers, or call sites that
issue names. Add new dated per-issue entries to whichever part is closest in
issue number, not in AGENTS.md.

This part covers issues #3492 through #5816: the `analyze_infra_relationships`
/ `what_deploys` scope-predicate arc (#3492, #3507, #3519, and the #5623
MATCHES_STATE family, in strict chronological order -- each entry builds on
"the fix above" from the previous one) split out of AGENTS.md's "Invariants
this package enforces" section, plus the pre-existing package-registry
correlation and dependency-chains entries preserved with headings demoted one
level (`##` to `###`) from an earlier single-file AGENTS.md, plus one later
addition out of chronological order: a P1 review follow-up to #5764 (grouped
here with the other "review follow-up" corrections rather than in part 3,
where #5764's main entry lives, for line-budget reasons -- part 3's own
pointer to it says so). See
[AGENTS-evidence-history.md](AGENTS-evidence-history.md) (part 1, issues
#2048-#4794/#4733) and
[AGENTS-evidence-history-3.md](AGENTS-evidence-history-3.md) (part 3, issues
#5761 and #5764).

## Evidence

### `analyze_infra_relationships` honors `relationship_type` (#3492)

No-Regression Evidence: the filter narrows the single per-entity relationship
read; it neither widens the seed-node lookup nor adds a query. Baseline = the
pre-#3492 bare `OPTIONAL MATCH (n)-[r]->(target)` / `(source)-[r2]->(n)`
whole-relationship pattern; after = the same pattern with an inline
`[r:TYPE...]` relationship-type predicate. Backend NornicDB (default canonical
graph backend per AGENTS.md), Neo4j compatibility mode unaffected. Input shape:
one `RunSingle` anchored on `n.id = $entity_id`, returning two collected
relationship slices for a single entity; the filter can only shrink the matched
edge set, so terminal row/queue counts are unchanged or lower and no extra
round trip is added. The inline relationship-type predicate is index-served by
the NornicDB relationship-type index, the same shape the relationships-catalog
count/edge routes already rely on. Proof: `go test ./internal/query -run
'TestInfraRelationships|TestResolveInfraRelationshipTypes' -count=1` (filtered
vs unfiltered Cypher, scoped-token coexistence, 400 on unknown) and `go test
./internal/query ./internal/mcp ./internal/relationships ./internal/telemetry
-count=1`.

Observability Evidence: the read now opens span `query.infra_relationships`
(`telemetry.SpanQueryInfraRelationships`, registered in `telemetry/registry.go`
and pinned by `telemetry.TestSpanNames`) carrying the stable `http.route` /
`eshu.capability` attributes plus a low-cardinality `eshu.relationship_filter`
attribute (`all` when unfiltered, else the pipe-joined edge types) so an
operator can confirm at 3 AM whether a `relationship_type` argument narrowed
the read. No new metric label is added; the filter value is a span attribute,
not a metric dimension, so cardinality stays bounded.

### `what_deploys` spans the full deployment edge family (#3507)

No-Regression Evidence: pure accuracy fix; the change only widens the
`what_deploys` alias edge-type set, it adds no query and changes no other
alias. Baseline = `what_deploys` filtering to `[r:DEPLOYS_FROM]` (dropping
`DEPLOYMENT_SOURCE`); after =
`[r:DEPLOYS_FROM|DEPLOYMENT_SOURCE|HAS_DEPLOYMENT_EVIDENCE]`. Backend NornicDB
(Neo4j compat unaffected); input shape unchanged — one `RunSingle` anchored on
`n.id = $entity_id` returning two collected relationship slices for one entity.
A wider type-union in the inline pattern is still index-served by the NornicDB
relationship-type index and only changes which edges match; it adds no round
trip and no broad scan, so terminal row/queue counts stay bounded by the same
single-entity neighborhood. Proof: `go test ./internal/query -run
'TestWhatDeploysSurfacesRuntimeDeploymentSourceEdge|TestResolveInfraRelationshipTypes|TestInfraRelationships'
-count=1` (failing-first DEPLOYMENT_SOURCE regression) and `go test
./internal/query ./internal/mcp -count=1`.

No-Observability-Change: reuses the #3492 span `query.infra_relationships` and
its `eshu.relationship_filter` attribute (now reports the wider pipe-joined
deploy set for `what_deploys`); no new span, metric, label, or log is added.

### Scope predicate admits the deployment-source topology (#3519)

Widening `what_deploys` (above) surfaced a second scope bug:
`infraResourceScopePredicate` previously authorized a neighbor only by
`repo_id` or the
`(:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n)`
USES path; the Repository neighbor of a `DEPLOYMENT_SOURCE` edge carries `id`,
not `repo_id`, and is not a USES target, so the scope-filtered relationship
read dropped the edge under a scoped token even when the repository was in
grant. Both new disjuncts are anchored on the indexed `Repository.id` grant
filter, and node ids are namespaced (`repo:` vs `tf:`/`k8s:`/`workload:`), so
the `id`-grant disjunct is inert for non-Repository nodes and never widens
their authorization.

No-Regression Evidence: scope-correctness fix; the predicate gains two
fail-closed disjuncts and removes none. Baseline = predicate with `repo_id` +
USES-path disjuncts (dropping in-grant `DEPLOYMENT_SOURCE` Repository
neighbors); after = same plus `id`-grant and DEFINES/INSTANCE_OF disjuncts.
Backend NornicDB (Neo4j compat unaffected); the predicate renders only in
scoped mode, so unscoped infra search/aggregate/relationship Cypher is
byte-identical and unchanged. The new EXISTS subquery mirrors the existing
workloadScopePredicate DEFINES anchor (indexed `Repository.id`), adding no
unbounded scan; it runs per candidate neighbor exactly like the existing USES
disjunct, so the bounded single-entity neighborhood cost class is unchanged.
Proof: `go test ./internal/query -run
'TestScopedWhatDeploys|TestInfraResourceScopePredicateAdmitsDeploymentTopology'
-count=1` (in-grant edge returned, out-of-grant excluded, predicate shape
pinned) and `go test ./internal/query ./internal/mcp -count=1`.

No-Observability-Change: the scope predicate is a Cypher WHERE fragment with no
span, metric, label, or log surface; the relationship read still reports the
#3492 `query.infra_relationships` span and `eshu.relationship_filter`
attribute.

### Scope predicate admits `TerraformStateResource` via `MATCHES_STATE` (#5623)

`TerraformStateResource` (#5443, state-observed Terraform resources) carries no
`repo_id`; before this fix none of `infraResourceScopeCoreDisjuncts`'s
disjuncts admitted it, so it was invisible to every scoped-token infra read
(fail-closed coverage gap, not a leak). Proven live on the pinned NornicDB
image: a property-only disjunct returned 2 rows (matched + unmatched) for a
fixture with exactly 1 matched-and-granted node, while the edge-traversal
disjunct correctly returns 1. Added to the shared core
(`infraResourceScopeCoreDisjuncts`), not gated like the DEFINES disjunct,
because a `TerraformStateResource` can have at most one MATCHES_STATE edge (the
config-match resolver anchors on a single resolved `OwningRepoID` and excludes
ambiguous matches from the edge write), so there is no name-collision
over-exposure risk for direct-projection callers such as
`relationshipEndpointScopePredicate`.

No-Regression Evidence: pure coverage fix; the predicate gains one fail-closed
disjunct and removes none. Baseline = predicate without the MATCHES_STATE
disjunct (TerraformStateResource always invisible to scoped tokens); after =
same plus the MATCHES_STATE inline-map term. Backend NornicDB (Neo4j
compatibility unaffected); the new disjunct is inert (empty pattern match) for
every other label in `allInfraLabels`, since only TerraformStateResource has
inbound MATCHES_STATE edges, so unscoped and non-state-resource scoped Cypher
shape and cost are unchanged. The new term is one more inline-map OR-branch,
same O(grant) cost class and cap (`maxScopeGrantInlineTerms`) as the existing
USES/DEFINES disjuncts — no new round trip, no unbounded scan. Proof: `go test
./internal/query -run
'TestInfraResourceScopePredicateComposesShapeAAndRejectsForbiddenShapes' -v
-count=1` (predicate shape pinned) plus the live regression `go test -tags
live_infra_scope_shape ./internal/query -run
TestLiveInfraScopeShapeMatchesStateDiscriminates -count=1` against an isolated
NornicDB (matched+granted visible, cross-tenant matched excluded, unmatched
excluded despite a matching config_repo_id property) and `go test
./internal/query -count=1`.

No-Observability-Change: the scope predicate is a Cypher WHERE fragment with no
span, metric, label, or log surface; no new telemetry signal is added or
needed.

### MATCHES_STATE disjunct's "at most one edge" invariant closed a real tenant-visibility leak, not just a theoretical one (#5623 P0 review follow-up)

The first #5623 patch's version of that retract skipped on delta cycles
(copying the node-level retract's `DeltaProjection` guard without re-deriving
whether the reasoning transferred); it did not. A state resource reassigned to
a DIFFERENT owning repo on a delta cycle got its NEW `MATCHES_STATE` edge
written immediately (the MERGE has no `DeltaProjection` guard) but kept its OLD
edge until the next full reconciliation generation (hours away per
`ESHU_REPO_RECONCILE_INTERVAL_HOURS`), so it carried edges to two different
repos simultaneously and this predicate admitted it for EITHER repo's grant --
including the repo that no longer owns it. The fix removed the
`DeltaProjection` skip from that retract (kept only the `FirstGeneration`
skip).

No-Regression Evidence: closes a real tenant-isolation gap, widening exactly
one retract's trigger condition and narrowing nothing else. Baseline =
`terraformStateMatchesConfigEdgeRetractStatements` skipped on `FirstGeneration
|| DeltaProjection`; after = skipped only on `FirstGeneration`. Backend
NornicDB (Neo4j compatibility unaffected); the Cypher statement itself is
byte-identical, only the Go condition that decides whether to emit it changed,
so the non-delta (full reconciliation) path is unchanged and already-passing.
Proof (failing-first, RED via `git apply -R` on the fix / GREEN restored, both
cited in the PR): `go test ./internal/storage/cypher -run
'TestTerraformStateMatchesConfigEdgeRetractStatementsRunsUnderDeltaProjection|TestTerraformStateMatchesConfigEdgeRetractStatementsSkipsOnFirstGeneration|TestTerraformStateMatchesConfigEdgeRetractStatementsRunsOnNonDeltaGeneration'
-v -count=1` (statement-shape unit proof) plus two live regressions against an
isolated NornicDB, both run through the REAL `CanonicalNodeWriter.Write`
pipeline across a full generation then a delta-cycle ownership reassignment
(not a raw seeded fixture): `go test ./internal/storage/cypher -run
TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive -count=1`
(this test has no build tag -- gated only by ESHU_CYPHER_BOLT_DSN, matching
every other `_live_test.go` in that package) (proves the stale edge is gone
after the delta cycle, and that a partial-failure retry of the same generation
is idempotent) and `go test -tags live_infra_scope_shape ./internal/query -run
TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment
-count=1` (proves the scope predicate in THIS package reflects only the current
owner afterward) and `go test ./internal/storage/cypher ./internal/query
-count=1`.

No-Observability-Change: both the retract Cypher and this package's scope
predicate remain Cypher fragments with no span, metric, label, or log surface;
no new telemetry signal is added or needed.

### The delta-cycle retract fix above wiped a still-valid edge on an ordinary resolver hiccup (#5623 P1 review follow-up)

`TerraformStateOwnershipResolver.ResolveOwningRepoID` fails closed on ANY
resolver error -- an ordinary transient Postgres timeout or pool exhaustion,
not only a genuine "no owner" -- and every `cmd/*` wiring site
(`cmd/bootstrap-index`, `cmd/ingester`, `cmd/projector`'s
`terraform_state_ownership.go`) treats that identically to "no owner,"
returning `row.OwningRepoID == ""`. The state resource's node still gets
upserted that cycle regardless, so on a delta cycle where a resolver hiccup hit
a resource whose node was still upserted, the retract could not distinguish
"ownership genuinely changed" from "we simply failed to learn it this cycle" --
it deleted the existing, still-correct `MATCHES_STATE` edge either way, and
nothing rewrote it (the MERGE excludes `OwningRepoID == ""` rows). Fail-closed
(under-authorization, never a leak) but a real accuracy regression on every
delta cycle instead of only full-reconciliation cycles. A row with
`OwningRepoID == ""` this cycle is simply excluded from the uid set, so its
existing edge survives untouched -- symmetric with the MERGE, which already
excludes the same rows for the same reason.

No-Regression Evidence: fail-closed accuracy fix; narrows the retract's uid set
to a strict subset of what it touched before (rows with resolved ownership),
never widens it. Baseline = every row this generation upserted is a retract
candidate regardless of resolution outcome; after = only rows with
`OwningRepoID != ""` are candidates. The Cypher statement gains one `AND s.uid
IN $uids` clause; the resolved-ownership path (the common case) is unaffected
in count or shape. Proof (failing-first, RED via `git apply -R` on this fix
alone -- keeping the delta-cycle fix above applied -- confirmed FAIL for the
right reason; GREEN restored): `go test ./internal/storage/cypher -run
'TestTerraformStateMatchesConfigEdgeRetractStatementsExcludesUnresolvedOwnershipRows|TestTerraformStateMatchesConfigEdgeRetractStatementsRunsUnderDeltaProjection|TestTerraformStateMatchesConfigEdgeRetractStatementsRunsOnNonDeltaGeneration|TestBuildTerraformStateStatementsRetractsEdgeBeforeMerge'
-v -count=1` (unit proof: all-unresolved emits nothing, mixed
resolved/unresolved includes only the resolved uid, resolved-ownership path
unchanged) plus a live regression against an isolated NornicDB, run through the
REAL `CanonicalNodeWriter.Write` pipeline across a full generation then a delta
cycle where ownership resolution returns not-ok (not a raw seeded fixture): `go
test ./internal/storage/cypher -run
TestCanonicalNodeWriterPreservesMatchesStateEdgeOnResolverHiccupDeltaCycleLive
-count=1` (this test has no build tag, matching every other `_live_test.go` in
that package). Re-ran the delta-cycle-reassignment P0 regressions
(`TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive` and
`TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment`)
alongside this fix to confirm it does not reopen the delta-cycle leak the fix
above closed -- both still pass. Also `go test ./internal/storage/cypher
./internal/query ./cmd/... -count=1`.

No-Observability-Change: the retract remains a Cypher WHERE/DELETE fragment
with no span, metric, label, or log surface; no new telemetry signal is added
or needed.

### NoOwner/AmbiguousOwner must retract too, not just Resolved (#5623 P1 review follow-up to the fix above)

A backend that previously resolved to a repo and later became unowned or
ambiguous kept that repo's `MATCHES_STATE` edge indefinitely -- the #5623 P0
tenant-visibility leak, reintroduced through a narrower door. The
classification (mapping a `*tfstatebackend.Resolver` result to this outcome) is
centralized in the new `internal/relationships/tfstatebackend/canonicalwriter`
package rather than duplicated across the three `cmd/*` adapters, which now
each delegate in one line.

No-Regression Evidence: widens the retract's candidate set from "resolved rows
only" back toward (but not identical to) the pre-#5623-P1 "every row" set --
Resolved, NoOwner, and AmbiguousOwner are all retract-eligible now; only
TransientFailure stays excluded. Proof (failing-first, RED via a temporary
one-line revert of the filter to `row.OwningRepoID == ""`, confirmed FAIL for
the right reason with the reassignment/hiccup cases unaffected; GREEN
restored): `go test -tags live_infra_scope_shape ./internal/query -run
TestLiveInfraScopeShapeMatchesStateFormerOwnerExcludedOnAuthoritativeNonOwner
-v -count=1` (both NoOwner and AmbiguousOwner subtests; proves THIS package's
scope predicate no longer authorizes the former owner) run together with
`TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment`
and `go test ./internal/storage/cypher -run
TestCanonicalNodeWriterRetractsMatchesStateEdgeOnAuthoritativeNonOwnerDeltaCycleLive
-v -count=1` (both subtests) run together with the #5623 P0/P1 siblings in that
package. See `internal/storage/cypher/AGENTS-evidence-history.md`'s own `#5623
P1 follow-up` entry for the full unit and package-boundary detail.

No-Observability-Change: the scope predicate and the retract both remain Cypher
fragments with no span, metric, label, or log surface; no new telemetry signal
is added or needed.
### Package-registry correlation pagination and authz-gate invariant (#5461/#5816)

Same failure class as #4733 above, now on the package-registry correlation
read and authorization path. `package_registry_correlations.go` decodes
`reducer_package_ownership/consumption/publication_correlation` facts through
the typed `factschema.Decode*` seam
(`factschema_decode_package_correlations.go`); a row whose payload is missing
its required `package_id` drops with a classified decode error instead of
surfacing an empty-identity row.

`PackageRegistryCorrelationPage` (`package_registry_correlation_page.go`)
derives `Truncated`/`NextCursorCorrelationID` from the RAW fetched fact
count/fact_id sequence -- never from `len(Rows)` -- mirroring
`buildWorkItemEvidencePage`. It also carries `WindowFactCount`, the raw
pre-decode count of facts in the visible window, distinct from `Truncated`:
`Truncated` only answers "does more exist past the window," while
`WindowFactCount` answers "how many facts are actually in the window
regardless of decode outcome" -- the predicate the authz gates below need.

Two callers in `package_registry_scoped_access.go` read
`PackageRegistryCorrelationPage` to decide whether a scoped caller may see an
anchored package, and BOTH must read `page.WindowFactCount`, never
`len(page.Rows)`:

- `packageRegistryGateForVisibility` grants on `page.WindowFactCount > 0`. A
  correlation fact that exists but fails typed decode must still grant --
  main's pre-#5461 hard-error-on-any-decode-failure behavior would have
  surfaced the problem loudly instead of silently denying a genuinely granted
  caller.
- `packageRegistryGateForVisibilityBatch` treats the batch as `ambiguous`
  (triggering an individual per-candidate re-verify) when
  `page.WindowFactCount >= packageRegistryMaxLimit || page.WindowFactCount >
  len(page.Rows)`. The first disjunct is the pre-existing at-cap crowd-out
  check (kept, but reading the raw count so it still fires even when a
  crowding candidate's facts partially fail decode). The second disjunct is
  new: a fact inside the window that fails decode carries no `PackageID`, so
  it can never land in `grantedSeen`; below the cap this would otherwise
  silently deny the one candidate whose only evidence was that dropped fact,
  exactly the same failure shape as an unambiguous `len(Rows) < cap` read
  before this fix.

A caller-side fake for `PackageRegistryCorrelationStore` that hand-builds an
already-decoded `Rows` slice (matching `WindowFactCount` to `len(Rows)`)
cannot exercise either regression: it structurally cannot model "the raw
window contains a fact that fails decode." Regression coverage for this class
routes fact bytes through the real `buildPackageRegistryCorrelationPage`
decode path instead
(`candidateFactPackageRegistryCorrelationStore` in
`package_registry_scoped_access_windowfactcount_test.go`), the same pattern
the existing `rawFactPackageRegistryCorrelationStore` in
`package_registry_correlations_pagination_test.go` uses for the pagination
fix. A store double that only ever hands back pre-decoded rows is blind to
this whole failure class -- prefer a raw-fact-based double over a
Rows-only one whenever a test needs to prove decode-drop behavior on this
package's correlation read path.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -count=1`
pass. `TestPackageRegistryGateForVisibilityGrantsWhenOnlyCorrelationFactFailsDecode`,
`TestPackageRegistryGateForVisibilityBatchReverifiesCandidateWhoseOnlyFactFailsDecode`,
and `TestPackageRegistryGateForVisibilityBatchAtCapWithDecodeDropStillReverifies`
fail against the pre-fix `len(page.Rows)`-based `granted`/`ambiguous` reads
(proven by temporarily reverting `packageRegistryGateForVisibility` and
`packageRegistryGateForVisibilityBatch` to that shape) and pass after reading
`page.WindowFactCount`.

No-Observability-Change: no route, graph write, queue, worker, runtime knob,
or metric instrument/label is added. The existing `pkgreg.correlation_grant`
and `pkgreg.name_anchor_batch_ambiguous` span attributes are unchanged in
shape; only the value they are computed from changed from a decode-affected
count to the raw fetched fact count.

### Dependency-chains publisher-truncation signal (#5816 finding on #5461)

`ResolvePackageDependencyChains` (`package_registry_dependency_chains.go`)
resolves two independently-bounded reads: a phase-1 consumption page anchored
on the consumer repository, and a phase-2 batched publication/ownership read
(`loadPackagePublishers`) keyed by every distinct package the phase-1 page
consumed, capped at `packageRegistryMaxLimit`. Before this fix,
`loadPackagePublishers` issued that batched read and only ever consumed its
`Rows`, discarding the returned `Truncated` flag: a package with more
publisher/ownership facts than the cap silently lost publishers beyond it from
every chain, with no signal anywhere in the response.

`loadPackagePublishers` now returns `(map[string][]PackageDependencyChainPublisher,
bool, error)` -- the bool is `publisherPage.Truncated` from the batched read.
`ResolvePackageDependencyChains` threads it into
`PackageDependencyChainPage.PublishersTruncated`, and
`package_registry_dependency_chains_handler.go`'s `listDependencyChains` puts
it on the wire as `publishers_truncated`, following this package's established
per-leg `<leg>_truncated` naming convention (for example
`code_relationships_nornicdb.go`'s `outgoing_truncated`/`incoming_truncated`
and `supply_chain_sbom_attachments.go`'s `component_evidence_truncated`)
rather than overloading the existing top-level `truncated`, which callers
already read as "phase-1 consumption paging only." The two flags are
independent by construction: a page can be `Truncated=false` (every
consumption correlation for the request fit) while `PublishersTruncated=true`
(one of those packages individually has more publisher facts than the batched
read's cap), because the two reads are bounded separately. Do not fold them
into one flag or infer one from the other.

Like the correlation-pagination fix above, a hand-built fake that returns
already-decoded `PackageRegistryCorrelationPage{Rows: ...}` with a
manually-set `Truncated` field can prove the propagation wiring but not the
underlying pagination contract; regression coverage instead uses
`rawFactPackageRegistryCorrelationStore`
(`package_registry_correlations_pagination_test.go`) so the publisher page is
built by the real `buildPackageRegistryCorrelationPage` from raw fact bytes
(`package_registry_dependency_chains_publishers_truncated_test.go`).

No-Regression Evidence: `go test ./internal/query -run
'TestPackageRegistryDependencyChainsHandlerReportsPublishers' -v -count=1`
covers both the over-cap (`publishers_truncated=true`, chain still returns the
capped publisher set) and under-cap (`publishers_truncated=false`, every
publisher present) cases.

No-Observability-Change: no route, graph write, queue, worker, runtime knob,
or metric instrument/label is added; the change is a new additive response
field derived from an existing bounded Postgres read.

### Repository story row bound reintroduced the fabricated-value defect (P1 review follow-up to #5764)

The `repositoryStoryStringRowLimit` bound introduced above (500 rows, added
by #5764 itself) reintroduced exactly the defect #5764 exists to remove: on a
repository with more than 500 workloads, the graph read landed on the LIMIT,
`queryRepositoryStoryGraphSummary` returned the capped list with no
indication anything was clipped, and `workload_count`/`platform_count` (the
story's `graph_summary` stage log, `repository_story.go`'s "%d workload(s)
and %d platform signal(s)" narrative, and `buildRepositoryStory`'s own
narrative sentence) are `len()` of these bounded lists, NOT separate
`count()` queries -- only `file_count` and `dependency_count` come from the
separate exact counts in `repository_context_counts.go`. Runtime-proven on a
600-workload repository: `status=200
deployment_summary="500 workload(s) and 0 platform signal(s)"
answer_metadata.truncated=false` with no truncation reason in
`partial_reasons` -- an affirmative false claim that nothing was clipped.

Fixed three ways. First, `queryRepositoryStoryStringRows`
(`repository_story_counts.go`) now requests
`repositoryStoryStringRowLimit + 1` rows and reports EXACT truncation
(`len(rows) > repositoryStoryStringRowLimit`) instead of the ambiguous
`len(rows) == limit` check, capping to the limit in Go; `getRepositoryStory`
(`repository.go`) appends the new `storyRowsTruncatedReason`
(`"story_rows_truncated"`) to `storyLimitations` when truncated, and
`buildRepositoryStoryResponseWithCoverage` (`repository_story.go`) sets the
response's top-level `"truncated"` field so `attachAnswerMetadata`'s
`BuildAnswerMetadata` (which reads `data["truncated"]` directly, not the
limitations slice) stops answering `answer_metadata.truncated=false` when
rows were actually clipped. Second, `queryRepositoryStoryWorkloadNames`'s
Cypher gained `RETURN DISTINCT w.name` (it previously had none): two
Workload nodes can share a name, and before this fix
`queryRepositoryStoryStringRows`'s own seen-map dedup ran AFTER the
LIMIT/truncation cap, so a repository with many duplicate-name Workload rows
could starve a different, real workload name out of the story once the LIMIT
landed mid-duplicate-run -- the identical starvation risk
`queryRepositoryStoryPlatformTypes`'s `RETURN DISTINCT p.type` already
guarded against. Third, `queryRepoInfrastructureFromGraph`
(`repository_infrastructure.go`) was switched from the ambiguous
`len(rows) == repositoryInfrastructureEntityLimit` idiom to the same
limit+1/exact-detection idiom, matching
`queryRepoDeploymentEvidenceDirection`'s (`repository_deployment_evidence.go`)
pre-existing precedent, for consistency across every bounded auxiliary read
on these two routes.

No-Regression Evidence: mutation-proven failing-first for every changed
assertion (apply mutation, confirm non-empty `git diff --numstat`, confirm
the `-run` filter matched, confirm RED for the fabricated-value/starvation/
ambiguous-boundary reason, then revert to GREEN): `go test ./internal/query
-run
'TestQueryRepositoryStoryStringRowsBoundsRowsWithNamedLimit|TestQueryRepositoryStoryStringRowsDetectsExactTruncation|TestQueryRepositoryStoryWorkloadNamesDistinctPreventsStarvation|TestGetRepositoryStoryRowsTruncatedIsDisclosed|TestGetRepositoryStoryRowsHealthyUnderLimitDoesNotDisclose|TestQueryRepoInfrastructureFromGraphBoundsRowsWithNamedLimit|TestQueryRepoInfrastructureFromGraphSignalsTruncationAtLimit|TestQueryRepoInfrastructureFromGraphNoTruncationAtLimit'
-v -count=1` and `go test ./internal/query ./internal/mcp ./internal/queryplan
-count=1`.

No-Observability-Change: the `repository_story.stage_completed` `graph_summary`
stage log gained a `slog.Bool("truncated", ...)` attribute mirroring the
existing convention (`service_workload_resolution.go`, `repository_stats.go`,
and this same file's `infrastructureDegradeLogAttrs`); no new span, metric
instrument, or metric label was added.
