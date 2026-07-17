# #5274 — semantic-search retained-index proof

Issue #5274 removes the repeated Postgres corpus/vector transfer and in-memory
index build from stable semantic and hybrid search requests. It does not change
ranking, result shape, the existing 500-document request corpus bound, or the
durable vector schema.

## Representative retained corpus

The proof used the largest ready retained repository scope available locally:

- 41,601 active curated search documents;
- 41,601 ready `local-hash-v1` vectors;
- 256 dimensions per vector;
- the production 500-document semantic-request bound;
- PostgreSQL 18.4 with no pgvector extension installed.

The current request path transferred 500 document rows, 500 vector-metadata
rows, and 500 vector-value rows for every query. The decoded documents contained
649,443 JSON bytes and the float64 vectors contained 1,024,000 raw bytes. That
is at least 1.67 MB per request before metadata, row, protocol, and Go object
overhead.

Warm component measurements on the retained scope were:

| Stage | Warm time |
| --- | ---: |
| document rows | 64–98 ms |
| vector metadata | 8.6–19 ms |
| vector values | 10.5–15.6 ms |
| map and validation | 0.4–0.9 ms |
| in-memory index build | 10–14.9 ms |
| scoring | 0.38–0.47 ms |

The repeated fetch and build were therefore the dominant avoidable work;
scoring was not.

## Alternatives proved before implementation

### Server-side float-array cosine

An exact SQL cosine scan over the existing double-precision arrays returned the
same top 11 but took 403–561 ms. It was four to five times slower than the
existing warm request and was rejected.

### pgvector exact and HNSW

A disposable `pgvector/pgvector:0.8.2-pg18-trixie` instance loaded the same
41,601 vectors. Import took about 0.65 seconds, HNSW construction took 4.09
seconds, the table occupied about 109 MB, and the HNSW index occupied about 56
MB. Twenty representative term queries compared top-11 results against the
exact baseline.

| Retrieval | p50 | p95 | Mean recall | Minimum recall | Disposition |
| --- | ---: | ---: | ---: | ---: | --- |
| exact pgvector scan | 5.7–7.55 ms | 6.3–9.35 ms | 1.0000 | 1.0000 | exact, but requires a new extension/image contract |
| HNSW `ef_search=100` | 0.89 ms | 1.16 ms | 0.7545 | 0.0000 | rejected: incorrect neighbor set |
| HNSW `ef_search=400` | 3.31 ms | 4.19 ms | 0.9636 | 0.8182 | rejected: not equivalent |
| HNSW `ef_search=1000` | 6.36 ms | 9.51 ms | 0.9682 | 0.8182 | rejected: not equivalent and no faster than exact |

The current sparse deterministic hash embeddings did not meet the exactness
contract under HNSW. Exact pgvector was promising as a future storage design,
but adding a database extension and deployment-image dependency was broader than
this request-path defect. The disposable proof container was deleted after the
measurement.

## Selected design and invalidation contract

API and MCP now keep a bounded process-local retrieval index. A cache key
contains the exact durable and request corpus identity:

- scope, canonical repository, and active generation;
- document projection revision and count;
- vector projection revision, build fence, and ready state;
- provider profile, source class, model, and vector-index version;
- normalized source/language filters, retrieval mode, corpus limit.

Every request reads this tuple from Postgres. A miss reads it again after the
corpus/index build and discards the build if anything changed. Empty, building,
failed, or revision-mismatched projections bypass the cache. There is no TTL,
negative caching, or stale-while-revalidate behavior.

The LRU defaults to eight entries and is capped at 32. Same-key misses coalesce;
different keys build concurrently. Cancellation of the build-owning request
does not strand a live waiter: when the owner is canceled or reaches its
deadline, the waiter retries with its own context. Restart behavior is a normal
cold miss against durable Postgres state.

## Finished same-data proof

`TestSemanticSearchIndexCachePGXLive` ran the production adapters on the same
retained scope and query. It compared every cached response with the uncached
result using exact Go value equality, counted storage calls, and measured ten
hits per run. Five independent runs produced:

| Path | Latency | Corpus rows transferred | Index builds |
| --- | ---: | ---: | ---: |
| uncached baseline | 113.4–127.4 ms | 500 docs + 500 metadata + 500 vectors | 1 per request |
| cache miss | 111.2–120.2 ms | 500 docs + 500 metadata + 500 vectors | 1 |
| cache hit median | 0.467–0.619 ms | 1 snapshot row | 0 |

All 50 cache-hit results and all five miss results exactly matched their
uncached baseline. Each run performed one document, metadata, and vector load
across the miss plus ten hits. The hit removes at least 1.67 MB of decoded corpus
payload and the 10–14.9 ms index construction while preserving the same answer.

## Concurrency and observability evidence

Focused race tests prove same-key coalescing, different-key parallel builds,
LRU eviction, projection-revision invalidation, filter-key normalization, and
live-waiter recovery after builder cancellation or deadline expiry.

The existing semantic-search request span now carries bounded
`search.index_cache` state (`hit`, `miss`, `coalesced`, `bypass_unready`, or
`retry_snapshot_changed`). The pre-request snapshot query uses the instrumented
Postgres store `semantic_search_snapshot`; existing document/vector stores and
route-duration telemetry remain unchanged.

## Verification

```bash
cd go
GOCACHE=/tmp/eshu-5274-gocache go test -race ./internal/query \
  -run 'TestSemanticSearchIndexCache|TestPersistedSemanticSearch|TestPostgresSemanticSearchSnapshot|TestSemanticSearchSnapshot' \
  -count=1

ESHU_SEMANTIC_SEARCH_CACHE_LIVE=1 ESHU_POSTGRES_DSN='<local retained DSN>' \
  GOCACHE=/tmp/eshu-5274-gocache go test ./internal/query \
  -run '^TestSemanticSearchIndexCachePGXLive$' -count=5 -v
```

The DSN is supplied from local retained-stack configuration and is never
printed or committed.
