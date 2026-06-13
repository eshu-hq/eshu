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

The route reads at most 500 active search documents before building the
in-process retrieval index. The response reports `indexed_document_count`,
`corpus_limit`, and `corpus_may_be_truncated` so clients can tell when the
bounded corpus may need a narrower anchor.

Results carry:

- `rank`, `score`, and `search_method`;
- the shaped search document with snake-case graph handles and entity refs;
- result graph handles for bounded follow-up calls;
- `truth_scope`, `freshness`, `failures`, and optional low-cardinality
  metadata.

Search scores and vector similarity stay derived evidence. They do not promote a
result to canonical graph truth.

## Modes

`keyword` uses BM25 over curated search-document text. `semantic` requires a
configured deterministic local embedder; without one the route returns
`backend_unavailable`. `hybrid` combines BM25 and vector ranks when an embedder
exists and may report `search_method=bm25` when no embedder is configured.

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
`search_method` show whether the request was scoped tightly enough and which
retrieval path answered.

## Related Docs

- [Search Retrieval Contract](../search-retrieval-contract.md)
- [Search Document Projection](../search-document-projection.md)
- [MCP Tool Contract Matrix](../mcp-tool-contract-matrix.md)
