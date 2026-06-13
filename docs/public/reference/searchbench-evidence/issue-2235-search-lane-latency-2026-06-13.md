# Issue #2235 — Search-lane latency benchmark (2026-06-13)

Measured run of `go/cmd/search-bench` comparing the current Postgres
content-search baseline against the in-process curated hybrid lane
(`internal/searchhybrid`) for keyword retrieval over a live Eshu content corpus.

This is a measured latency-and-cost record, not a fabricated `search-benchmark-evidence/v1`
JSON record: the v1 schema requires a NornicDB arm, which is not measurable here
(see "Unmeasured arms" below). Numbers below are real.

## Environment

- Eshu commit: `3d8dbb0e1`
- Postgres: `postgres:18-alpine` (remote-e2e QA stack, host port 15432)
- Canonical NornicDB: `timothyswt/nornicdb-cpu-bge:v1.1.3` with
  `NORNICDB_SEARCH_BM25_ENABLED=false`, `NORNICDB_SEARCH_VECTOR_ENABLED=false`,
  `NORNICDB_EMBEDDING_ENABLED=false` (graph-only policy per design 430).
- Command: `go run ./cmd/search-bench --queries 50 --rounds 5 --limit 20`

## Corpus

- Repository: `repository:r_9a84f5f1` (largest by entity count).
- Entity rows scanned: 27,583; file rows scanned: 1,122.
- Curated documents indexed: **27,822** (overflow 0).
- Skipped sensitive (secret-term match): 883; skipped excluded: 0; skipped
  missing-handle: 0.
- In-process index build time: ~0.12 s.
- Query shapes: 50 deterministic single-term queries derived from the most
  frequent entity-name tokens (≥4 chars), 5 rounds each (250 measured calls per
  backend).

## Measured latency (keyword retrieval)

| Backend | p50 | p95 | max | hits (round 1) |
| --- | --- | --- | --- | --- |
| `postgres_content_search` (`source_cache ILIKE`, repo-scoped) | ~1.1 ms | ~3.9 ms | ~132 ms | 960 |
| `in_process_hybrid_bm25` (`searchhybrid`, repo-scoped) | ~19.5 ms | ~22.3 ms | ~38.9 ms | 1050 |

(Two runs at 40×3 and 50×5 agreed within noise.)

## Reading

- The Postgres baseline has a **faster median** (~1 ms) but a **long tail**
  (p95 ~4 ms, max ~130 ms) from `ILIKE` scan/cache variance.
- The in-process hybrid lane is **consistent** (tight p50/p95/max) but **slower
  at median** (~20 ms) because the current `searchhybrid` implementation scores
  **every in-scope document linearly** per query — there is no inverted index. At
  ~28k documents that is ~20 ms; it scales linearly with corpus size and would
  need an inverted index before serving large corpora.
- Recall/precision/nDCG were **not** measured: they require a labeled query suite
  with expected handles, which this QA corpus does not have.

## Unmeasured arms

- **NornicDB search lane (`nornicdb_bm25`/`nornicdb_vector`/`nornicdb_hybrid`)**:
  not measured. The canonical NornicDB runs search-disabled, and no
  search-enabled curated NornicDB database/deployment with the curated documents
  indexed as `SemanticContext` nodes exists yet (design 430 Option B/C). Standing
  that up is prerequisite work, not a number that can be recorded today.

## Recommendation

`defer_search_change`.

The measured evidence does not justify enabling NornicDB search by default — the
design-430 stop-threshold (do not slow or complicate canonical graph readiness)
is not cleared, and the NornicDB arm is unmeasured. The Postgres content-search
baseline remains the search lane. Before the in-process hybrid lane can replace
or augment it, two gaps must close: (1) an **inverted index** so latency does not
grow linearly with corpus size, and (2) a **labeled query suite** so recall and
ranking quality — the in-process hybrid's actual advantage — can be measured, not
just latency. The `searchhybrid` backend and this harness make both achievable;
re-run with those in place to record a quality-based decision.
