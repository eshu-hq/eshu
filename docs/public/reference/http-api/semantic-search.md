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
| `languages` | Optional filter over language values (e.g. `go`, `python`, `typescript`). Documents are included only when their `Labels` array contains `language:<lang>` for one of the requested values. An empty array means no language filter. Any non-empty token is accepted; an unmatched language returns an empty result set rather than an error. The index is the source of truth for which language values exist. |
| `rerank` | Opt into graph-neighborhood reranking over the in-scope results. Off by default. |

The route reads the active generation from the persisted curated search index.
The reducer maintains that index when it writes search documents, so request
handling does not rebuild a full corpus index. The response reports
`indexed_document_count`, `corpus_limit`, and `corpus_may_be_truncated`;
`corpus_limit=0` means there is no request-time corpus cap, while `limit` still
bounds returned results.
API, MCP, and reducer share the semantic-search embedder selector. Setting
`ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` or `local_hash` forces the
deterministic no-network profile. `auto_hash` uses that local profile only when
no governed `search_documents` provider profile is configured. When unset,
exactly one governed `search_documents` provider profile may supply embeddings
if the profile declares source policy, model id, endpoint profile id, credential
source, and positive `embedding_dimensions`; multiple eligible profiles require
`ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID`. The reducer builds
active-generation vector sidecar rows and the API uses those persisted rows for
`semantic` and `hybrid` modes only when the stored vector identity matches the
selected provider profile id, source class, model id, dimensions, content hash,
and vector index version. Missing, stale, partial, rebuilding, failed,
incompatible, or malformed vector state returns explicit degraded state instead
of claiming semantic readiness.

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

## Language facet

The response always includes a `facets` block:

```json
{
  "facets": {
    "languages": {
      "go": 4,
      "python": 2
    }
  }
}
```

`facets.languages` maps each recognized language label (the `<lang>` part of
`language:<lang>`) to the count of results in the returned set carrying that
label. Facet counts are derived from the post-filter result set — the documents
already returned by the bounded index query — so no second scan is issued.

**Note:** `facets.languages` counts are page-scoped (over the returned result
set), not corpus-wide totals. A language with documents in the corpus but none
in the returned page will not appear in the facet map.

Performance Evidence: the facet is computed by iterating the already-returned
result slice; it adds O(results × labels-per-doc) work with no additional
database query. No-Observability-Change: the `telemetry.SpanQuerySemanticSearch`
span already wraps the full route; no new metric or span is added for facet
computation.

## Graph-neighborhood reranking

When `rerank: true` is set, the route reorders the already-retrieved, in-scope
results around code-to-cloud graph anchors before returning them. Reranking is a
permutation of the retrieved set: it never adds, drops, or relabels a result, so
scope and authorization filtering still apply and truth labels are unchanged.
Graph proximity is derived only from the graph handles already on each curated
document, so the route issues no extra graph read or write.

A reranked response adds:

- top-level `rerank` with `state` (`applied`, `inactive`, or `stale_skipped`)
  and `applied`;
- per-result `ranking_basis` carrying the preserved `baseline_rank` and
  `baseline_score`, the new `final_rank`, the summed `graph_boost`, and the
  `contributions` (each a signal `kind`, the bounded `kind:id` `handle`, and a
  `weight`);
- top-level `recommended_next_calls`: bounded first-class read tools
  (`get_service_story`, `trace_deployment_chain`, `get_incident_context`,
  `explain_supply_chain_impact`, `build_evidence_citation_packet`) to advance the
  investigation from the results.

Reranking fails closed to the baseline order when it is not requested
(`state` omitted), when no graph signal fires for any result
(`state=inactive`), or when graph context is stale (`state=stale_skipped`). The
signals, weights, and fusion are documented in
[`internal/searchrerank`](https://github.com/eshu-hq/eshu/tree/main/go/internal/searchrerank).
The accept decision and measured nDCG lift are recorded in
[issue-2678 graph-rerank evidence](https://github.com/eshu-hq/eshu/blob/main/docs/internal/evidence/searchbench-evidence/2678-graph-rerank.md).

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
transport-only: it forwards the request fields, including `rerank`, to
`POST /api/v0/search/semantic` and preserves the HTTP envelope as
`structuredContent`. The reranked `rerank`, `ranking_basis`, and
`recommended_next_calls` fields flow back unchanged.

## Evidence

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(SemanticSearch|OpenAPISpecIncludesSemanticSearch|SemanticSearchTool)' -count=1`
covers required fields, bounds, scoped-token no-grant and not-found behavior,
OpenAPI shape, MCP schema, route mapping, graph-neighborhood reranking
(promotion of the service-anchored result, ranking basis, recommended next
calls, and the rerank-off default), language filter narrowing, unknown language
open-pass (200 with empty result set), facet count accuracy, and no-filter
no-op behaviour.

Language Filter and Facet Evidence: `go test ./internal/query/ -run 'TestSemanticSearchHandler(Language|Facets|Passes)' -count=1`
and `go test ./internal/storage/postgres/ -run 'TestEshuSearchIndexStore(Language|NoLanguage)' -count=1`
verify the SQL predicate is present when languages are requested, absent when
not, and that label values arrive as parameterised args (no interpolation).
The unknown-language open-pass behaviour (200 with empty result set, index
reached) is verified by
`TestSemanticSearchHandlerUnknownLanguageReturnsEmptyResult`.

Benchmark Evidence: `go test ./internal/searchrerank -run Benchmark -v -count=1`
scores baseline vs reranked nDCG@3 over a labeled fixture suite and gates the
accept decision recorded in
[issue-2678 graph-rerank evidence](https://github.com/eshu-hq/eshu/blob/main/docs/internal/evidence/searchbench-evidence/2678-graph-rerank.md):
mean nDCG@3 0.7232 -> 1.0000 with no regression and the no-signal case held
neutral.

Observability Evidence: `telemetry.SpanQuerySemanticSearch`
(`query.semantic_search`) wraps the route with stable `http.route` and
`eshu.capability` attributes. Response fields `indexed_document_count`,
`corpus_limit`, `corpus_may_be_truncated`, `truncated`, and per-result
`search_method` plus top-level `retrieval_state` show whether the request was
scoped tightly enough and which retrieval path answered.

Observability Evidence: when persisted vector retrieval is enabled, API and MCP
wire the vector metadata and vector value sidecar reads through Postgres
`InstrumentedDB` store labels `semantic_search_vector_metadata` and
`semantic_search_vector_values`. The existing
`eshu_dp_postgres_query_duration_seconds{operation="read",store=...}` metric
and `postgres.query` child spans separate slow vector-sidecar reads from the
outer semantic-search route span. Verified by
`TestNewRouterWiresLocalSemanticHybridVectorStoresWithPostgresInstrumentation`
and
`TestNewMCPQueryRouterWiresLocalSemanticHybridVectorStoresWithPostgresInstrumentation`.

## Related Docs

- [Search Retrieval Contract](../search-retrieval-contract.md)
- [Semantic Hybrid Search Admission](../semantic-hybrid-search-admission.md)
- [Search Document Projection](../search-document-projection.md)
- [MCP Tool Contract Matrix](../mcp-tool-contract-matrix.md)
