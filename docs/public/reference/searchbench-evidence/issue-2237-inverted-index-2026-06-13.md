# Issue #2237 follow-up — searchhybrid inverted index (2026-06-13)

The first #2235 run (latency record
[issue-2235-search-lane-latency-2026-06-13.md](issue-2235-search-lane-latency-2026-06-13.md))
showed the in-process curated hybrid lane scored **every in-scope document
linearly** per query (~20 ms p50 at ~28k documents). This change replaces that
scan with an inverted index (term → postings), so a keyword/BM25 query visits
only the documents that contain its terms.

## Performance Evidence

Same harness (`go/cmd/search-bench`), same corpus (`repository:r_9a84f5f1`,
27,822 curated documents), 50 derived queries × 5 rounds, keyword mode.

| Backend | p50 before | p50 after | p95 before | p95 after | max before | max after |
| --- | --- | --- | --- | --- | --- | --- |
| `in_process_hybrid_bm25` | ~19.5 ms | **~0.53 ms** | ~22.3 ms | **~5.3 ms** | ~38.9 ms | **~15.3 ms** |
| `postgres_content_search` (reference) | ~1.1 ms | ~1.1 ms | ~3.9 ms | ~1.9 ms | ~132 ms | ~151 ms |

The in-process hybrid p50 improved **~37×** (19.5 ms → 0.53 ms) and is now faster
than the Postgres baseline at the median with a tighter tail, while keeping BM25
ranking. Index build time is unchanged (~0.12–0.15 s for the corpus). The
micro-benchmark `BenchmarkBackendSearchHybrid` (every document matches every
term, the worst case for an inverted index) moved ~2.9 ms → ~2.2 ms, as expected:
the inverted index helps in proportion to query selectivity, and most real
queries are selective.

Correctness is held by `TestInvertedIndexMatchesDirectScoring`, which asserts the
postings-driven scores equal the direct per-document BM25 scores for the same
queries, and by the unchanged backend ranking/scope/overflow tests.

## No-Observability-Change

This is an internal retrieval-backend optimization with identical inputs and
outputs (same candidates, scores, ranking, and truncation). It adds no API/MCP
route, runtime flag, graph write, or telemetry signal; the bounded retrieval
runner's observation contract is unchanged.

## Remaining follow-ups for a quality-based decision

- Brute-force **vector** scoring still scans in-scope documents; an ANN index is
  a separate follow-up (the keyword/BM25 path no longer scans).
- A **labeled query suite** is still needed to record recall/ranking quality, not
  just latency.
- A **search-enabled NornicDB** deployment is still needed to measure the
  NornicDB arm.

The recorded #2235 decision remains `defer_search_change`; this strengthens the
in-process lane's latency profile for when that decision is revisited.
