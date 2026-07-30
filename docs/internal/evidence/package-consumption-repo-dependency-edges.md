# Package-Consumption Repo Dependency Edges Evidence

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Package-Consumption Repo Dependency Edges

`PackageSourceCorrelationHandler` now joins its package consumption decisions to
the exact/derived owner and publisher decisions (on package id) and projects
consumer-repo `DEPENDS_ON` owner-repo edges through the shared repo-dependency
projection lane (`DomainRepoDependency`), reusing `BuildSharedProjectionIntent`
and the existing `RepoDependencyIntentWriter`. The join is gated on a wired
intent writer: when nil (no repo-dependency lane configured) the handler stays
fact-only and projects nothing, so the package-registry deployment profile is
unchanged (#3579, unblocks #3504).

Only `exact`/`derived` owner resolutions are projected; `ambiguous`,
`unresolved`, `stale`, and `rejected` outcomes carry no single indexed owner and
are dropped. Self-references (consumer == owner) are dropped. Multiple packages
resolving to the same consumer/owner pair collapse to one edge whose
`evidence_count` records the backing package count. The edge carries the
distinct `evidence_source = projection/package-consumption` (separate from
`resolver/cross-repo`), so `groupRepoDependencyUpsertRows` projects and retracts
it as its own per-source group without colliding with cross-repo edges. The
intent id is a deterministic hash of the acceptance identity (`scope_id`,
`generation_id`, a stable `package_consumption_repo_dependency:<scope>:<gen>`
`source_run_id`) plus the `repo:<consumer>-><owner>` partition key, so
re-projecting the same scope yields the same intent id and the downstream
`DEPENDS_ON` MERGE is idempotent under retries and re-projection. The wall clock
feeds only `created_at`, never the id hash. The acceptance source-run id is a
stable function of the package-registry scope only (not the generation), so the
shared repo-dependency lane treats a new generation's edge as a refresh of the
prior edge on the same consumer acceptance unit.

No-Regression Evidence: `go test ./internal/reducer ./internal/telemetry
-count=1` — the focused `TestBuildPackageConsumptionRepoDependencyIntents*`,
`TestBuildPackageConsumptionRepoEdgeRefreshIntents*`,
`TestPackageConsumptionRepoEdgeSourceRunID*`, and
`TestPackageSourceCorrelationHandlerEmitsRefreshIntentWhenOwnerDisappears` cases
cover the owner edge, publication-derived edge,
ambiguous/unresolved/self/missing-consumer skips, multi-package dedupe
(`evidence_count = 2`), cross-generation acceptance-key stability (idempotent
refresh), and refresh-on-disappear (a consumer that resolves no owner emits a
package-consumption retraction so its stale edge is removed). The join is bounded O(C) over the
scope's package consumption decisions against an O(1) owner map built once from
the exact/derived owner/publisher decisions; it introduces no full-fleet scan,
no per-edge graph round trip, and no new Cypher — projection reuses the
unchanged shared repo-dependency MERGE lane, so the hot path is unchanged.

Observability Evidence: the handler emits the new
`eshu_dp_package_consumption_repo_edges_total` counter dimensioned by reducer
`domain` (`repo_dependency`) and `outcome` (`projected` for emitted edges,
`skipped_no_owner` for consumers that declared package dependencies but resolved
no owner this generation and instead emit a refresh/retraction), so an operator
can confirm the package-consumption projection lane is producing edges and see
which consumers were refreshed without an owner. The `EvidenceSummary` also
appends `repo_dependency_edges=<n>` per package-source correlation result. The
acceptance source-run id is a stable function of the package-registry scope only
(`package_consumption_repo_dependency:<scope>`), never the generation, so the
shared repo-dependency lane reconstructs the same consumer acceptance unit across
generations: a new generation refreshes the consumer's package-consumption edges
in place, and a consumer whose owner disappears emits a retraction so the stale
edge is removed instead of orphaned.
