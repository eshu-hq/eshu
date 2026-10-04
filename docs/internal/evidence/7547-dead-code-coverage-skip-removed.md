# #7547 dead-code coverage skip removed

## Scope

`DeadCodeIncomingEntityIDs` (`go/internal/query/codequery/deadcode/incoming.go`) skipped the producer-anchored legacy incoming read whenever a repository's reachability watermark read "available and not truncated". The reachability rows are a root-seeded traversal, so an entity with no row is only proven unreachable if the roots were adequate, and the watermark does not say that: a repository with zero roots still stamps one. This change removes the skip. The snapshot rows are still merged first, and the legacy one-hop read now always runs for the entities the snapshot did not answer (a hidden-consumer-only entry still counts as unanswered). There is no root-ratio, count, or language heuristic, no new index, and no migration. The fallback is the legacy read, not `ambiguous`. The arbiter ruling on issue #7547 is the design record. Refs #7249.

## Accuracy proof

The test that encoded the old behavior is rewritten to the new contract and renamed `TestDeadCodeIncomingEntityIDsCompleteSnapshotStillReadsLegacyEdgesForUnansweredEntities`. A second test, `TestFilterDeadCodeResultsKeepsNonRootCalleeReachableUnderCompleteSnapshot`, classifies at the filter layer: a complete, non-truncated snapshot with no row for either candidate, one candidate whose only caller is a non-root function (completed legacy edge), and one orphan. RED on the unmodified code:

```text
--- FAIL: TestDeadCodeIncomingEntityIDsCompleteSnapshotStillReadsLegacyEdgesForUnansweredEntities (0.00s)
    reachability_coverage_test.go:49: incoming = map[string]code.DeadCodeIncomingEdge{}, want dead-a to carry its legacy incoming edge
--- FAIL: TestFilterDeadCodeResultsKeepsNonRootCalleeReachableUnderCompleteSnapshot (0.00s)
    reachability_coverage_test.go:104: classified unused = []string{"callee-of-non-root", "orphan"}, want []string{"orphan"}: a symbol with a completed legacy incoming edge is not unused
```

After the change both pass, the orphan with no incoming edge of any kind stays unused, and the truncated-snapshot and hidden-only tests are unchanged and pass.

## Replica differential (read-only counts)

Read-only PostgreSQL probes on the QA replica counted candidate-type entities (`Function`, `Class`, `Struct`, `Interface`, `Trait`, `SqlFunction`) that have no active-generation reachability row with `depth > 0` but do have a completed legacy incoming edge (`code_calls` callee, `USES_METACLASS` target, or `inheritance_edges` parent) in the active generation. Under the old logic, for a repository whose watermark was not truncated, every one of these was classified as having no incoming edge; under the new logic each one has an incoming edge. This query did not read `truncated`; the arbiter's census found no active watermark truncated on QA. Counts only; no data was copied out and nothing was written.

| Repository | Candidate-type entities | With a reachability row | Without a row | Flip: active-run legacy edge | Flip: any-generation edge |
| --- | ---: | ---: | ---: | ---: | ---: |
| Largest repository (by file count) | 45,569 | 15 | 45,554 | 27,421 | 27,425 |
| A repository whose active watermark has zero rows | 675 | 0 | 675 | 481 | 481 |

The largest repository has one active watermark and 15 reachability-answered entities out of 45,569. At the time of the read there were 790 active watermarks, 378 of them with zero reachability rows. The largest zero-row repository by file count has no candidate-type entities, so a smaller repository with candidates represents the zero-row class.

Limits: the flip column counts all candidate-type entities, before the handler's root, test, generated-code, and exclusion policy, so it is an upper bound on how many returned candidates change class, not a count of changed API results. The filter also runs a graph incoming probe for some labels, which under the old logic could already have kept a few of these. It does not say how many flipped entities are truly live; it says the old logic ruled them unused with no root-adequacy proof. The differential ran the same statement shape three times (largest repository, the zero-row repository with no candidates, then the zero-row repository with candidates) because the first zero-row pick was empty; the extra runs touched a repository of 675 entities and cost 106 and 135 ms.

## Cost

Performance Evidence: this branch is based on `origin/main` without PR #7554, where the legacy read in `content_reader_dead_code.go` has no generation or completeness bound. On this base, every repository whose snapshot reads complete (most of the roughly 790 active watermarks) pays that unbound legacy read on the general path for its unanswered entities, about 3.1 s in the worst recorded case, until #7554 merges. This PR must not enter the merge queue before #7554 is merged; the coordinator enforces that merge order, and the PR body carries the same sentence. Once #7554 lands, the general path pays its bounded statement per unanswered entity page. Two recorded read-only `EXPLAIN (ANALYZE, BUFFERS)` runs on the largest repository for a 101-id page give the bounded cost: the statement as built by the Go code in PR #7554 took about 84 ms on a custom plan and 282 ms on a generic plan (the cache-matched generic pair went from 3,148 ms to 282 ms); a separate IN-lists versus pair-EXISTS comparison on the prepared statement took 270 to 313 ms on a generic plan. Both are replica statement times on the largest repository from one data copy. Delta or incomplete repositories still take the unbound read (3.1 s worst case), recorded as the accuracy-first cost. These figures are reported from the earlier shims and were not re-measured here. The deployed handler before/after and endpoint p95 are not measured here and follow the merge of this change and #7554.

No-Observability-Change: the change removes one `code_reachability_coverage` read from the dead-code incoming path and adds no metric, span, log key, status field, worker, or queue stage. The existing `postgres.query` span and `db.operation=dead_code_incoming_entity_ids` label now cover more calls; an operator sees the shift as more `dead_code_incoming_entity_ids` operations per dead-code request.

The deployed endpoint result after both this change and #7554 are rolled out is recorded in the "Deployed acceptance sweep" section of `docs/internal/evidence/7249-dead-code-reachability.md`: all 21 swept pairs under 1 s in the second run, with one disclosed 1.005 s repeat in the first.

## Rollback

Revert the commit. No schema, data, or contract changed. `CodeReachabilityCoverage` and the content reader's coverage method stay in place for the codequery seam alias and PR B; no production caller reads them after this change.

## Not in this change

This change is PR A of #7547 (PR #7559, merged): the reader no longer skips the legacy read. The reducer-side writer fix (the depth-10 cutoff with unseen targets, and a zero-root repository stamped `truncated = true`) is PR B. Still open as a follow-up that waits on QA measurement: adopting PR #7554's completeness predicates (full generation, both work items succeeded, no pending intents) with a drift test against the query constant. A skip may return only behind `exact` language maturity plus a completeness proof; no hook is built now.

## Reducer watermark stamps and the epoch bump

The reducer now stamps `truncated = true` for a zero-root snapshot and for a depth cutoff where a node at `MaxDepth` has an edge to an unvisited entity. The runner logs the reason as `truncation_reason` (`no_roots`, `max_visited`, `max_depth`).

PR #7570 stamps new snapshots with these semantics. The epoch bump (`CodeReachabilityVerdictSchemaEpoch` 3 to 4) is in its own change, slice E of #7547, so already-stamped watermarks re-project once and are re-stamped. Until that change deploys and its drain finishes, watermarks already stamped keep their old `truncated` value. The cost, drain estimate, watch plan, and rollback are in [7547-reachability-epoch-4-bump.md](7547-reachability-epoch-4-bump.md).

Consumer effect: nothing on `origin/main` reads `watermark.truncated` today, so no served answer changes with this change (the single-repo reader stopped consulting the watermark in #7559, and `CodeReachabilityCoverage` has no non-test caller). Once the companion cross-repo reader (PR C) lands, an unscoped cross-repo dead-code request reads as unknown for a repo whose watermark is stamped truncated, instead of as proven dead. A zero-root repo leaves that state only when it gains a root.

Log and counter: `snapshots_truncated` now also counts zero-root snapshots. To keep it from burying real signals, the per-snapshot `truncation_reason = no_roots` line logs at INFO, while `max_visited` and `max_depth` stay at WARN.

Proof is at unit level in `go/internal/reducer/codeintel`: the stamp tests cover zero roots (nil and blank-only), a frontier past depth 3 and past the default depth 10, a complete chain exactly at the bound, a back-edge to a visited node, a diamond, a multi-root frontier, both bounds firing (`max_visited` wins), all-downgraded Rails controller roots (stamped `no_roots` by the runner), and the log severity per reason. The stamp tests were seen failing on behavior before the implementation.

No-Regression Evidence: local microbenchmark, base `791078e81` versus this head, interleaved pairs, `-cpu=1`, `-count=10` per side, compared with benchstat. `BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff` (new in this change: a 50,000-node 4-ary tree walked to `MaxDepth` 6, so about four thousand nodes sit at the cutoff with unvisited children) took 11.22 ms ±5% on base and 11.55 ms ±3% on head, no significant difference (p=0.19, n=10); bytes and allocations per op are identical (15.73 MiB, 102,450 allocs). The existing `BenchmarkBuildCodeReachabilityRows` (12 deep, never reaches the cutoff) took 59.36 ms ±86% on base and 63.42 ms ±19% on head (p=0.68); its spread on base is too wide to read anything from, so it is reported, not relied on. Bytes and allocations are identical there too. Scope and limits: these figures come from one developer laptop under other load, not the remote full-corpus host (unreachable during this work), so they are a no-regression check for the in-memory walk and not a repo-scale or wall-time claim. The scan adds at most one pass over the outgoing edges of nodes popped at `MaxDepth`, with one map lookup per such edge (it does not stop early, so the benchmark above pays the full scan). No SQL, lock, claim, queue or worker path is added, and no claim is made about re-projection cost, because no re-projection is triggered here.

Arbiter ruling (arbiter-eshu, 2026-10-04, PR B of #7547): this interleaved local microbenchmark is accepted as the no-regression record because the touched path is the pure in-memory `BuildCodeReachabilityRowsWithStats` walk (no SQL, graph, lock, claim, queue, or worker path), the claim is a same-machine relative check (no absolute, repo-scale, or wall-time claim), and the remote validation host was unreachable (ssh to port 22 timed out at 2026-10-04T05:49Z). The added scan costs at most one map lookup per outgoing edge of each queue entry popped at `MaxDepth`, one extra edge-level pass bounded by the edge count with zero allocation. The remote rerun of `BenchmarkBuildCodeReachabilityRowsFrontierAtCutoff` (base `791078e81` versus this head, `-cpu=1 -count=10`, benchstat) was owed when the VPN returned; it has since run on the remote host and is recorded in the epoch-4 evidence note: +3.6% on the frontier-at-cutoff benchmark (p<0.001), +2.3% on the older benchmark, identical allocations, which supersedes the laptop figures above.

Observability Evidence: no new metric, span, status field or worker. The existing truncation line gains a `truncation_reason` attribute and a level that depends on it.
