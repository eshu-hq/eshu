# Semantic Search Route

The semantic search route runs bounded retrieval over active curated Eshu search
documents for one repository corpus. It is a derived-retrieval surface, not
canonical graph truth and not a whole-graph search fallback.

## `POST /api/v0/search/semantic`

Required request fields:

| Field | Meaning |
| --- | --- |
| `repo_id` | Repository id that bounds the searchable corpus and the durable search-document scope. |
| `query` | Search text. |
| `mode` | `keyword`, `semantic`, or `hybrid`. |
| `limit` | Explicit top-K result limit, max 100. |
| `timeout_ms` | Explicit server-side retrieval timeout. |

Optional request fields:

| Field | Meaning |
| --- | --- |
| `service_id` | Smaller service anchor inside the repository corpus. |
| `workload_id` | Smaller workload anchor inside the repository corpus. |
| `environment` | Environment anchor inside the repository corpus. |
| `source_kinds` | Optional filter over `code_entity`, `repository_file`, `runtime_summary`, and `semantic_context`. |

The route reads the active generation from the persisted curated search index.
The reducer maintains that index when it writes search documents, so request
handling does not rebuild a full corpus index. The response reports
`indexed_document_count`, `corpus_limit`, and `corpus_may_be_truncated`;
`corpus_limit=0` means there is no request-time corpus cap, while `limit` still
bounds returned results.
When `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` or `local_hash` is set on API
or MCP, `semantic` and `hybrid` requests load ready active-generation local
vector metadata and payload rows for the repository scope. The stored vector
identity must match the deterministic no-network hash embedder, active search
document content hash, and vector index version before the route reports vector
participation. Missing, stale, partial, rebuilding, failed, incompatible, or
malformed vector state falls back to keyword candidates with an explicit
`retrieval_state` / `vector_retrieval_state` instead of claiming semantic or
hybrid participation. That local path is bounded by `corpus_limit=500` and is
not a hosted-provider, graph-write, or external vector-store integration. Unset
runtime configuration keeps the persisted BM25 behavior.

Results carry:

- `rank`, `score`, and `search_method`;
- top-level `retrieval_state` (`keyword_only`, `semantic_unavailable`,
  `hybrid_degraded`, `semantic_active`, or `hybrid_active`);
- the shaped search document with snake-case graph handles and entity refs;
- result graph handles for bounded follow-up calls;
- `truth_scope`, `freshness`, `failures`, and optional low-cardinality
  metadata.

Search scores and vector similarity stay derived evidence. They do not promote a
result to canonical graph truth.

## Modes

`keyword` uses BM25 over persisted curated search-document postings and reports
`retrieval_state=keyword_only`. `semantic` requires the explicit local hash
embedder setting; without it the route returns `backend_unavailable` and no
document store read runs. With it, results report `search_method=vector` and
`retrieval_state=semantic_active` only when persisted vectors are ready and
compatible. `hybrid` without the setting serves the same persisted BM25 ranking
with `retrieval_state=hybrid_degraded`; with the setting, ready BM25 and vector
candidates are fused with Reciprocal Rank Fusion and results can report
`search_method=rrf_hybrid` plus `retrieval_state=hybrid_active`.

## Authorization

Repository scoped tokens are intersected before any search-document read:

- no repository grants return a successful empty result set;
- out-of-grant `repo_id` returns `not_found`;
- allowed repository grants read only that repository corpus.

This keeps repository existence and corpus shape from leaking across scoped
tokens.

## MCP Parity

MCP exposes the same route through `search_semantic_context`. MCP dispatch is
transport-only: it forwards the request fields to
`POST /api/v0/search/semantic` and preserves the HTTP envelope as
`structuredContent`.

## Evidence

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(SemanticSearch|OpenAPISpecIncludesSemanticSearch|SemanticSearchTool)' -count=1`
covers required fields, bounds, scoped-token no-grant and not-found behavior,
OpenAPI shape, MCP schema, and route mapping.

Observability Evidence: `telemetry.SpanQuerySemanticSearch`
(`query.semantic_search`) wraps the route with stable `http.route` and
`eshu.capability` attributes. Response fields `indexed_document_count`,
`corpus_limit`, `corpus_may_be_truncated`, `truncated`, and per-result
`search_method` plus top-level `retrieval_state` show whether the request was
scoped tightly enough and which retrieval path answered.

## Related Docs

- [Search Retrieval Contract](../search-retrieval-contract.md)
- [Semantic Hybrid Search Admission](../semantic-hybrid-search-admission.md)
- [Search Document Projection](../search-document-projection.md)
- [MCP Tool Contract Matrix](../mcp-tool-contract-matrix.md)
