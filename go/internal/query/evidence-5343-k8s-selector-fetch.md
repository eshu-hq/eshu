# Evidence: #5343 P2-1 remediation — narrow the k8s SELECTS candidate fetch

Scope: `go/internal/query/content_reader.go`,
`go/internal/query/content_reader_by_type.go`,
`go/internal/query/content_relationships.go`, `go/internal/query/ports.go`.

## Problem this remediates

The original #5343 change added real `matchLabels ⊆ pod-template labels`
selector matching to the SELECTS relationship, but code review (P2-1) found
that `buildOutgoingK8sSelectRelationships` and
`buildIncomingK8sSelectRelationships` fetched candidates via a **repo-wide**
`ListRepoEntities(repoID, repositorySemanticEntityLimit)` scan
(`ORDER BY relative_path, start_line LIMIT 5000`), then Go-filtered the result
down to `entity_type = 'K8sResource'`. In a repository with more than 5000
indexed content entities, `K8sResource` rows sorting past position 5000 in
that repo-wide order are silently dropped **before the type filter ever
runs** — a latent false negative: the SELECTS edge is missing with no error,
no log, and no query-time signal. The fetch also decodes JSONB metadata for
up to 5000 rows per candidate lookup regardless of how many are actually
`K8sResource`.

## No-Regression Evidence

No-Regression Evidence: this remediation is a behavior change (accuracy fix),
proven by the expected-delta regression tests and the EXPLAIN shim below, not
by output-identity to the pre-fix query.

**Accuracy delta first (this is the primary win, not a side effect):** the fix
adds `ContentReader.ListRepoEntitiesByType(ctx, repoID, entityType, limit)`
and switches both builders to it. The LIMIT now applies to the
**type-filtered** row set (`WHERE repo_id = $1 AND entity_type = $2 ORDER BY
relative_path, start_line LIMIT $3`) instead of the full per-repo row set, so
a rare entity type can no longer be pushed past the fetch horizon by
unrelated rows of other types. (The ORDER BY also carries an `entity_id`
tiebreaker for deterministic truncation, and the builders request
`repositorySemanticEntityLimit + 1` = 5001 rows so an exact `> limit` overflow
can be detected and disclosed; the EXPLAIN numbers below were captured at
`LIMIT 5000` — the one-row delta does not affect the row-count/buffer-hit
conclusion.)

- Regression test (Go, red-then-green, see the P2-1 commit):
  `TestBuildContentRelationshipSetK8sServiceSelectsRecoversDeploymentPastTruncationHorizon`
  and
  `TestBuildContentRelationshipSetK8sDeploymentRecoversIncomingServicePastTruncationHorizon`
  in `content_relationships_k8s_truncation_test.go`. Each seeds
  `repositorySemanticEntityLimit` (5000) non-`K8sResource` filler rows ahead of
  one matching `K8sResource` row in fetch order. Confirmed RED (0 edges) when
  the builder called `ListRepoEntities`, confirmed GREEN (1 edge, correct
  reason) after switching to `ListRepoEntitiesByType`, for both the outgoing
  (Service→Deployment) and incoming (Deployment←Service) directions
  (symmetry is load-bearing: switching only one builder would silently
  reintroduce an outgoing/incoming edge-count asymmetry above the fetch
  horizon).

**EXPLAIN ANALYZE theory shim (prove-the-theory-first, cheapest measurement):**
a throwaway `postgres:16` Docker container (`eshu-5343-explain-shim`, no
persistent volume, destroyed after the shim ran) was seeded with the
production `content_entities` schema and indexes
(`go/internal/storage/postgres/migrations/004_content_store.sql`,
`035_content_entities_repo_entity_idx.sql`) and a worst-case partition:
`repo-1` has 20,000 rows total — 19,800 non-`K8sResource` rows whose
`relative_path` sorts strictly BEFORE any `K8sResource` row, and 200
`K8sResource` rows (mixed `Deployment`/`Service`) with `relative_path`
prefixed `zzz-k8s-manifests/` so they sort at the tail. This reproduces
exactly the ordering shape the Go regression test proves: every
`K8sResource` row falls past a 5000-row repo-wide horizon.

OLD query (`WHERE repo_id = $1 ORDER BY relative_path, start_line LIMIT
5000`):

```
Limit  (actual time=0.120..2.913 rows=5000 loops=1)
  Buffers: shared hit=5533
  ->  Incremental Sort (actual time=0.119..2.773 rows=5000 loops=1)
        Sort Key: relative_path, start_line
        Presorted Key: relative_path
        ->  Index Scan using content_entities_path_idx on content_entities
              (actual time=0.008..1.538 rows=5001 loops=1)
              Filter: (repo_id = 'repo-1'::text)
Planning Time: 0.164 ms
Execution Time: 3.036 ms
```

K8sResource rows actually present in the OLD query's 5000-row result: **0 of
200 (100% dropped)**.

NEW query (`WHERE repo_id = $1 AND entity_type = 'K8sResource' ORDER BY
relative_path, start_line LIMIT 5000`):

```
Limit  (actual time=0.418..0.427 rows=200 loops=1)
  Buffers: shared hit=15
  ->  Sort (actual time=0.417..0.421 rows=200 loops=1)
        Sort Key: relative_path, start_line
        ->  Bitmap Heap Scan on content_entities
              (actual time=0.020..0.043 rows=200 loops=1)
              Recheck Cond: (entity_type = 'K8sResource'::text)
              Filter: (repo_id = 'repo-1'::text)
              ->  Bitmap Index Scan on content_entities_type_idx
                    (actual time=0.012..0.012 rows=200 loops=1)
                    Index Cond: (entity_type = 'K8sResource'::text)
Planning Time: 0.176 ms
Execution Time: 0.449 ms
```

K8sResource rows in the NEW query's result: **200 of 200 (100% recovered)**.

| Metric | OLD (`ListRepoEntities`) | NEW (`ListRepoEntitiesByType`) | Delta |
| --- | ---: | ---: | --- |
| Rows returned | 5000 | 200 | -4800 rows, -96% |
| Rows decoded (JSONB metadata) | 5000 | 200 | -4800 decodes, -96% |
| K8sResource rows recovered (worst case) | 0 / 200 | 200 / 200 | +200, the correctness win |
| Shared buffer hits | 5533 | 15 | -5518, -99.7% |
| Execution time | 3.036 ms | 0.449 ms | -85% (same shim, same data) |

**Honest framing on the plan shape:** there is no composite
`(repo_id, entity_type)` index — only the separate `content_entities_repo_idx`
(`repo_id`) and `content_entities_type_idx` (`entity_type`), confirmed by
reading `004_content_store.sql`. The planner used a Bitmap Heap Scan on
`content_entities_type_idx` with a `repo_id` recheck filter, not an
index-only intersection. The measured win here is **fewer rows returned,
fewer per-row JSONB metadata decodes, and fewer Go-side allocations** — not a
claim of a new composite index; none was added, and none is proposed by this
change. The 85% wall-time and 99.7% buffer-hit deltas above are a
consequence of the 96% row-count reduction in this worst-case partition (a
repo with 20k entities and 1% K8sResource share), not of a new access path.

**Identity regime (≤ `repositorySemanticEntityLimit` entities):** the existing
`content_relationships_k8s_test.go` and `content_relationships_k8s_match_test.go`
suites (six tests) run the real `ContentReader.ListRepoEntitiesByType` against
a fake SQL driver and pass unchanged post-fix — same edges, same reasons,
same order as before the P2-1 fix, since below the limit the type filter and
the LIMIT no longer interact.

**Backend/version:** backend-agnostic — Postgres-only change (content-store
read path), no NornicDB/Neo4j/Cypher involvement. Measured against
`postgres:16` in Docker; the production deployment target uses the same
`content_entities` schema and index set.

**Input shape / worst case:** a single repository indexed with more than
`repositorySemanticEntityLimit` (5000) content entities, where the
`K8sResource` rows are a small minority sorting near the tail of
`(relative_path, start_line)` order. This is the exact shape the shim
reproduces (20,000 total rows, 200 `K8sResource`, 1% share, worst-case
ordering).

## No-Observability-Change

No-Observability-Change: query-path only, no new or removed spans/metrics/logs.

This is a query-read-path correctness and read-cost fix only. No new
spans/metrics/logs were added, and none were removed.
`ListRepoEntitiesByType` reuses the same `postgres.query` span shape as every
other `ContentReader` method, with `db.operation = "list_repo_entities_by_type"`
distinguishing it from `list_repo_entities` in traces — an operator debugging
a slow or truncated k8s-selects query already has the tracing they need
without a new instrument. The only user-visible signal that moves is the
SELECTS edge count itself, which increases toward the correct (non-truncated)
matched set in the fixed worst-case regime; that is the intended accuracy
delta this remediation exists to produce, not a regression.
