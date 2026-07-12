# Evidence: incremental catalog merge instead of whole-cache eviction (#5129)

Issue [#5129](https://github.com/eshu-hq/eshu/issues/5129), from the #5122
under-20 attribution: the accepted 896-repo bootstrap run
(`3624-post5108-main-20260712T064007Z`) showed `catalog_cache_hit=false` on
all 896 `load_repository_catalog` commit stages and 896 cache invalidations —
382.6s of strictly serialized (measured max_concurrency=1) commit-chain time —
because every bootstrap scope onboards a new repository and the pre-#5129
cache evicted wholesale on each one. This change merges the committed
generation's repository identities into the cached catalog in place.

 Performance Evidence: prove-theory shim recorded on #5122 (900-commit
 merge-vs-reload set equivalence with 0 mismatches incl. alias drift,
 in-generation duplicates, and cross-repo alias collisions; catalog-matcher
 order-insensitivity across 27 orderings; merge 46.6µs/op vs the
 production-measured 0.427s serialized DB load × 895 avoided). Local proof of
 the finished change: `BenchmarkIngestionStoreCatalogLoadsPerCommit/onboarding`
 (new; bootstrap shape — every commit onboards a new repo,
 SkipRelationshipBackfill mirrors bootstrap-index wiring) on the same machine:
 OLD (origin/main eviction) 0.5000 catalog_loads/commit, 22.5ms/op, 255,497
 allocs/op vs NEW (merge) 0.005 catalog_loads/commit, 10.0ms/op, 102,487
 allocs/op — 100× fewer shared-cache loads, 2.24× faster, 2.5× fewer
 allocations on the harness. On the accepted production run shape (1.0
 evict-driven reload per onboarding commit) the merge removes all 895
 post-warm serialized reloads (~382s of commit-chain wall time,
 extrapolated from the measured 0.427s median load); the reference-machine
 full-corpus wall-clock confirmation is tracked by issue #5122, whose
 acceptance criteria require re-measuring the collection stage boundaries
 on the accepted machine/profile after its candidates land.
 Steady-state behavior is unchanged: `cached` bench stays at 0.005
 loads/commit, and unchanged-identity commits skip the merge entirely.

 No-Regression Evidence: full `internal/storage/postgres` ingestion-store
 test suite green under `-race`, including the #3481 O(1) reuse test, the
 #3521 P1 open-transaction load test, the #3521 P2 alias-drift accuracy test
 (re-pinned on cache content: drifted alias visible, stale alias gone,
 without a reload), the concurrency-safety test, and two new #5129 merge
 regression tests (onboarding commits leave shared-cache loads at the warm
 fill; alias drift merges in place). Accuracy contracts are asserted on
 cache CONTENT, not only load counts.

 Observability Evidence: the commit-stage log `repository_catalog_invalidated`
 is renamed to `repository_catalog_merged` (same stage position, same
 `current_generation_repo_count` attribute); `load_repository_catalog` keeps
 `catalog_cache_hit` and `catalog_loads_total`, which now stay at 1 load per
 process during bootstrap onboarding instead of climbing per commit. No new
 metric instruments or pipeline stages.

Cross-process visibility is unchanged by design: under eviction the cache
reloaded only when THIS process onboarded or renamed a repository, so other
processes' commits were never a reload trigger; corpus-wide catalog
completeness remains owned by the deferred `BackfillAllRelationshipEvidence`
pass, which always reloads fresh.
