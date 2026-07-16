# Semantic Search Scope and Readiness Evidence (#5245)

This note records the local retained-data proof for three failures on the same
semantic-search route: the public canonical repository id was used as the
internal ingestion scope, pgx could not decode the persisted `float8[]`, and a
valid ready projection with zero documents was treated as perpetually pending.

## Retained data shape

The read-only proof ran on 2026-07-14 against the retained local full stack on
PostgreSQL 18.4. The stack remained running throughout.

| Shape | Cardinality |
| --- | ---: |
| Active repository scopes | 427 |
| Active contentful repository scopes | 373 |
| Active indexed scopes | 373 |
| Active indexed search documents | 881,271 |
| Largest active scope | 41,601 documents |

No canonical repository id in the API-facing repository set is an ingestion
`scope_id`; the route therefore cannot query the durable index correctly by
copying `repo_id` into both fields.

## Old and new SQL shapes

The old route effectively used the authorized public id directly:

```sql
WHERE vec.scope_id = :canonical_repo_id
  AND document.repo_id = :canonical_repo_id
```

The new bounded resolver runs after the existing repository authorization
check and before any search read:

```sql
SELECT scope_id
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND active_generation_id IS NOT NULL
  AND payload->>'repo_id' = $1
ORDER BY observed_at DESC, scope_id ASC
LIMIT 2;
```

An empty mapping returns the existing successful empty result. One row supplies
the internal scope while retaining the canonical id as `repo_id`. Two rows fail
closed as ambiguous. Exact equality, not `LIKE` or prefix matching, prevents a
canonical id from selecting a sibling repository.

Scoped credentials may authorize the ingestion `scope_id` directly rather
than the canonical repository id. That path is validated independently with an
exact active-scope lookup before the index read:

```sql
SELECT COALESCE(payload->>'repo_id', '')
FROM ingestion_scopes
WHERE scope_kind = 'repository'
  AND active_generation_id IS NOT NULL
  AND scope_id = $1
LIMIT 1;
```

The authorized scope remains the index partition and the returned canonical id
remains the document-level repository filter. A missing, stale, or malformed
scope fails closed instead of returning an authoritative empty result for the
wrong partition.

The old search-document sweep completion predicate was fact presence OR index
stats presence. A valid zero-document projection has no search-document fact,
so it was selected on every 30-second sweep. The new predicate is:

```sql
AND NOT EXISTS (
    SELECT 1
    FROM eshu_search_document_projection_state projection
    JOIN eshu_search_index_stats idx
      ON idx.scope_id = projection.scope_id
     AND idx.generation_id = projection.generation_id
     AND projection.document_count = idx.document_count
    WHERE projection.scope_id = scope.scope_id
      AND projection.generation_id = scope.active_generation_id
      AND projection.state = 'ready'
)
```

That is the durable completion contract for both non-empty and zero-document
projections.

## Measured results

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on the same retained data
showed the exact canonical-id resolver returning one row in **0.101 ms** with
66 shared-buffer hits. The 1,050-row scope catalog is deliberately small; no
new expression index is justified. The old readiness query returned eight
false-pending scopes in **2.959 ms** with 3,731 shared-buffer hits. The new
readiness query returned zero scopes in **2.384 ms** with 1,736 shared-buffer
hits. This change is performance-neutral per sweep (0.575 ms faster) and is not
claimed as a seconds-scale optimization; its value is eliminating eight
redundant intents and misleading completion logs every 30 seconds.

| Proof | Old | New | Exact expectation |
| --- | ---: | ---: | --- |
| Largest-scope index visibility | 0 index rows | 1 active index row / 41,601 docs | canonical id maps to exactly one active scope |
| Known retained BM25 term | 0 candidates through public-id-as-scope | at least 1 candidate | active indexed document is returned |
| pgx persisted vector read | HTTP/store scan error | 1 vector row in 0.01 s | vector length equals declared dimensions |
| Search-document pending scopes | 8 | 0 | ready projection count equals index count, including 0=0 |
| Prefix collision | possible only with a widened predicate | exact `=` | no sibling-prefix match |
| Duplicate active mappings | arbitrary if limited to one | HTTP 409 ambiguous | fail closed before index read |

No-Regression Evidence: the authorization grant is checked before resolution,
so an out-of-grant repository still returns not-found without touching the
resolver or index. A direct scope grant is validated as an active repository
scope and supplies the canonical document filter without widening access. API
and MCP constructors both wire the same resolver into the shared handler.
`TestSemanticSearchScopeKnownTermPGXLive` proved both resolution directions on
the same retained active scope and returned a real BM25 candidate in 0.16 s.
`TestEshuSearchVectorValueListActivePGXLive`
proved the production pgx driver decodes the persisted `float8[]` through the
public store path. Focused unit tests cover authorized mapping, out-of-grant
ordering, ambiguity, exact SQL identity, vector decoding, and the zero-document
completion predicate.

## Concurrency and idempotency

The change does not alter workers, leases, claims, transaction boundaries, or
queue parallelism. The sweep conflict domain remains
`eshu_search_document`, and the entity key remains
`eshu_search_document:<scope_id>`. Work-item identity still includes scope,
generation, domain, and entity; the existing queue insert remains
`ON CONFLICT (work_item_id) DO NOTHING`. Concurrent sweepers therefore converge
on the same work item. A projection becoming ready after the pending snapshot
can cause at most one redundant idempotent enqueue, and the next sweep observes
the ready projection/index-count pair. No serialization is introduced.

Observability Evidence: API and MCP wire the canonical-scope lookup through the
existing `InstrumentedDB` contract with the stable low-cardinality store label
`semantic_search_scope`. Operators receive a `postgres.query` child span with
error status and the `eshu_dp_postgres_query_duration_seconds` histogram for
this lookup, in addition to the parent semantic-search request signals. Wiring
tests assert the tracer, instruments, and store label; the shared
`InstrumentedDB` tests cover success and query-error span status. No queue,
worker, status schema, or high-cardinality metric dimension changes. The
existing sweep-completed log no longer emits a false
`pending_scopes=8 enqueued_intents=8` record every 30 seconds after those scopes
have durably completed.

## Commands run

```bash
GOCACHE=/tmp/eshu-5245-gocache go test ./internal/query \
  -run 'Test(SemanticSearchHandler.*Scope|PostgresSemanticSearchScopeResolver)' \
  -count=1 -v

GOCACHE=/tmp/eshu-5245-gocache go test ./internal/storage/postgres \
  -run 'Test(ScanEshuSearchVectorValueDecodesPGXTextArray|EshuSearchDocumentPendingStore)' \
  -count=1 -v

GOCACHE=/tmp/eshu-5245-gocache go test ./cmd/api ./cmd/mcp-server \
  -run 'TestNew(Router|MCPQueryRouter).*Semantic' -count=1 -v

ESHU_SEMANTIC_SEARCH_SCOPE_LIVE=1 ESHU_POSTGRES_DSN="$RETAINED_DSN" \
  GOCACHE=/tmp/eshu-5245-gocache go test ./internal/query \
  -run '^TestSemanticSearchScopeKnownTermPGXLive$' -count=1 -v

ESHU_SEARCH_VECTOR_VALUES_SCAN_LIVE=1 ESHU_POSTGRES_DSN="$RETAINED_DSN" \
  GOCACHE=/tmp/eshu-5245-gocache go test ./internal/storage/postgres \
  -run '^TestEshuSearchVectorValueListActivePGXLive$' -count=1 -v
```

The two retained SQL statements above were also run with
`EXPLAIN (ANALYZE, BUFFERS)` through `psql -X -v ON_ERROR_STOP=1`; both were
read-only.
