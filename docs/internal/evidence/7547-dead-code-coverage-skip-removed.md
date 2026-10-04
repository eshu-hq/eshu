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

The reachability loader (PR B of #7547) is untouched here. The reducer-side watermark fixes (depth-10 cutoff with unseen targets, zero-root repository stamped `truncated = true`) landed separately in `go/internal/reducer/codeintel`. Still open: adopting PR #7554's completeness predicates (full generation, both work items succeeded, no pending intents) with a drift test against the query constant. A skip may return only behind `exact` language maturity plus a completeness proof; no hook is built now.

## Epoch bump to 4 (reducer watermark change)

Decision: the arbiter ruled on #7547 that `CodeReachabilityVerdictSchemaEpoch` goes from 3 to 4 in `go/internal/storage/postgres/code/reachability/store.go`. The reducer now stamps `truncated = true` for a zero-root snapshot and for a depth cutoff with unseen targets. That changes the truncated bit for unchanged inputs. A watermark stamped at epoch 3 carries the old `truncated = false`, and the loader only reschedules a repo on a stale `updated_at` or a lower epoch, so without the bump those watermarks would never be re-stamped. No DDL, backfill SQL, loader change, worker change or batch change.

Cost estimate (unmeasured on QA): the QA census read 790 active watermarks, 378 of them with zero rows and at most 7 depth-cut. After the bump every epoch-3 watermark is stale once, so roughly 790 re-projections run one time. At the default batch limit of 100 that is about 8 reducer cycles. Each re-projection reads the repo's roots and edges and replaces its rows, as any normal projection does.

Owner-visible consequence: after the bump the roughly 378 zero-root consumers stay `truncated = true`, so an unscoped cross-repo dead-code request reads as unknown for those repos instead of as proven dead. That is the intended result. A repo leaves that state only when it gains a root.

Fixture-scale proof (throwaway `postgres:18-alpine`, Docker; not QA data, not QA scale):

- `TestCodeReachabilityTruncationEpochBumpRestampsWatermarks` (`go/internal/storage/postgres/code_reachability_upgrade_backfill_live_test.go`) seeds three repos at epoch 3, `truncated = false`, `updated_at` newer than the last completed intent: a zero-root repo, an 11-hop chain, and a rooted complete repo. After one `ProcessOnce` the zero-root repo is `truncated = true` with logged `truncation_reason = no_roots`, the chain is `truncated = true` with `max_depth`, and the complete repo is `truncated = false`. All three are at epoch 4. `code_reachability_rows` for the complete repo is identical before and after (`EXCEPT ALL` both ways returns 0). A second pass does not reschedule any of the three. The test runs in 0.15 s. The "before" rows for the complete repo come from a first projection by the same code, then restoring the epoch-3 stamp, so this proves idempotent re-projection, not an independent baseline.
- Seeded mutation: setting the constant back to 3 turns that test RED (`epoch-3 watermark for ... was not re-scheduled at epoch 3`) and turns `TestCodeReachabilityVerdictSchemaEpochBumpedForTruncationSemantics` RED (`want >= 4`).
- `TestCodeReachabilityPendingInputsPlanAtEpochBump` (`go/internal/storage/postgres/code/reachability/store_route_liveness_live_test.go`) runs `EXPLAIN (ANALYZE, BUFFERS)` of `listPendingCodeReachabilityInputsSQL` over 800 fixture watermarks, all stale at epoch 4. The plan has the same node classes as the same query at epoch 3 (all current): Limit, Sort, GroupAggregate, Nested Loop, Hash Join, index scans on the intents lookup index, the generation primary key and the watermark primary key. Epoch 3 runs in 7.4 ms (0 rows), epoch 4 in 8.5 ms (100 rows, top-N heapsort), at 7229 and 7227 shared buffer hits. The loader walks every accepted repo on every call today, and it still does; the bump does not change that.

No-Regression Evidence: the epoch 4 plan above keeps the node classes of the epoch 3 plan at fixture scale (800 repos, 7.4 ms to 8.5 ms); the one-time re-projection cost on QA is estimated, not measured.

No-Observability-Change: the bump adds no metric, span, log key or status field. The `truncation_reason` attribute on the existing truncation warning came with the reducer change above.
