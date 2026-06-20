# Semantic Hybrid Search Admission

This contract defines when Eshu may serve production `semantic` or `hybrid`
search from a real embedder-backed retrieval path. It is a gate for
implementation work under #2451. It does not add an embedder, runtime flag,
provider call, API change, MCP change, graph write, or vector index by itself.

For the measured quality and latency thresholds that decide when an
embedder-backed path is *production-grade* (versus working-but-degraded), see the
[Hybrid Retrieval Production Gate](hybrid-retrieval-production-gate.md).
Hosted search embedders also require the
[Hosted Search Embedder Gate](hosted-search-embedder-gate.md) before any
provider or gateway traffic is enabled.

## Truth Boundary

Semantic search is derived retrieval evidence over curated
`EshuSearchDocument` rows. Search rank, embedding similarity, vector distance,
BM25 score, and Reciprocal Rank Fusion score must not create or overwrite
canonical graph truth.

The production path remains:

```text
curated search document -> governed embedding -> vector/BM25 retrieval
  -> bounded response -> optional graph follow-up through existing handles
```

Search results may point to existing graph handles for bounded follow-up reads.
They must not create services, workloads, deployments, incident links,
ownership, dependencies, or confidence-bearing correlation edges.

## Admission States

The semantic-search route and MCP tool must report a stable retrieval state so
operators and clients know which path answered.

| State | Meaning |
| --- | --- |
| `keyword_only` | Persisted BM25 answered; no embedder/vector path participated. |
| `semantic_unavailable` | Semantic mode was requested but no governed embedder is configured. |
| `hybrid_degraded` | Hybrid was requested but fell back to keyword-only behavior with an explicit method and failure class. |
| `semantic_active` | A governed embedder and vector index answered semantic retrieval. |
| `hybrid_active` | BM25 and vector candidates both participated in fusion. |
| `policy_denied` | Provider profile, source policy, ACL, budget, or egress policy denied embedding. |
| `index_unready` | Vector index is missing, stale, partial, or still building. |

No-provider mode is valid. It should preserve deterministic keyword behavior
and return explicit unavailable or degraded state for semantic and hybrid
requests rather than hiding the absence of embeddings.

## Embedder Contract

A production embedder implementation must be behind a narrow port. It may use a
local model, hosted provider, internal gateway, or other approved provider only
after governance admits the `search_documents` source class and scope. Hosted
provider and gateway adapters live outside `go/internal/searchhybrid`; that
package keeps the deterministic fusion boundary and must not import hosted SDKs
or make egress calls.

The embedder path must record:

- provider profile id or local profile class, never raw credentials
- model id or version
- source class and scope
- redaction policy version
- token, byte, cost, and concurrency budgets
- retention posture for prompt/input and response/vector metadata
- failure class and retryability
- embedding dimension and vector schema version

The embedder must reject raw provider keys, token-bearing endpoints, private
hostnames, local machine paths, and unredacted source values in committed
configuration, status payloads, logs, metric labels, docs, and PR text.

Hosted search-embedding adapters must not be implemented until the
[Hosted Search Embedder Gate](hosted-search-embedder-gate.md) is closed or
explicitly waived by the owning reviewers. Until then, repo-local work is
limited to deterministic local embeddings, fail-closed contracts, and
documentation that performs no provider traffic.

## Runtime Admission

API and MCP semantic/hybrid search can use the real hybrid backend only when all
of these are true:

1. The request has a bounded repository scope, query, mode, limit, and timeout.
2. The active generation has curated search documents.
3. A governed embedder is configured for the `search_documents` source class.
4. Source policy, ACL, egress, redaction, budget, and retention checks pass.
5. The vector index is built for the active generation and matching embedding
   schema version.
6. The route emits retrieval state, method, index freshness, and truncation
   metadata in the response.
7. A drift-guard test proves the production API and MCP path use the hybrid
   backend when the embedder and vector index are configured.

If any condition fails, the route must return the documented unavailable or
degraded state. It must not silently claim semantic or hybrid retrieval when
only keyword results participated.

## Vector Index Proof

Large-corpus semantic or hybrid readiness requires an ANN or equivalent bounded
vector retrieval index. Brute-force scans are acceptable only for fixture tests,
small local proofs, or benchmark baselines that state the corpus size.

The API/MCP/reducer semantic-search selector has two admitted paths. An
explicit `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` setting forces the
deterministic no-network local profile. When that override is unset, exactly one
governed `search_documents` provider profile may supply embeddings if source
policy, credential source, endpoint profile id, model id, and
`embedding_dimensions` are configured. Multiple eligible profiles require
`ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` and fail closed without it.

Ready persisted vectors use `VectorRetrievalAuto`: exact cosine below the
staged ANN threshold and the in-process angular-LSH candidate index with exact
cosine reranking above it. This satisfies the bounded in-process ANN admission
path for curated search documents, but it is still not canonical graph truth or
an external vector-store readiness claim.

Implementation proof must record:

- repository or scope count
- indexed document count and vector count
- embedding dimension and schema version
- vector index build duration
- query p50/p95/p99 latency by mode
- recall, precision, nDCG, and false-canonical-claim metrics from the
  searchbench suite
- index stale, partial, rebuilding, or failed states
- CPU, memory, and storage impact

The proof must compare the same query suite against the persisted keyword
baseline and the semantic or hybrid candidate.

## Graph-Neighborhood Reranking

Reranking (`internal/searchrerank`) is an opt-in stage that reorders the
already-retrieved, in-scope results around code-to-cloud graph anchors. It is
admitted under the same truth boundary as retrieval: it is a permutation of the
retrieved set that never creates, removes, or relabels a result, and it derives
graph proximity only from the graph handles already on each curated document, so
it performs no extra graph read or write and promotes no score to canonical
truth.

A request opts in with `rerank: true`. The response then reports a `rerank.state`
that an operator and client can rely on:

| Rerank state | Meaning |
| --- | --- |
| `applied` | At least one graph signal fired and the results were fused into a graph-aware order. |
| `inactive` | Reranking was requested but no graph signal fired; the baseline order is returned unchanged. |
| `stale_skipped` | Graph context was stale, so reranking failed closed to the baseline order. |

Reranking must fail closed (return the baseline order with an explicit state)
whenever it is not requested, no signal fires, or graph context is stale. It must
never reorder results it was not given, and the per-result `ranking_basis` must
preserve the baseline rank and lexical/vector score. Adoption requires measured
benchmark evidence that reranking improves quality without regressing the
no-signal case; the accept decision and nDCG lift are recorded in
[issue-2678 graph-rerank evidence](searchbench-evidence/2678-graph-rerank.md).

## Failure Classes

Search responses, status, logs, and metrics must use bounded failure classes:

- `provider_not_configured`
- `policy_denied`
- `acl_denied`
- `budget_exhausted`
- `redaction_failed`
- `embedder_unavailable`
- `embedding_dimension_mismatch`
- `vector_index_missing`
- `vector_index_stale`
- `vector_index_building`
- `vector_index_partial`
- `semantic_timeout`
- `hybrid_degraded`
- `unsupported_mode`

Failure classes are diagnostic metadata. They do not change truth labels or
canonical graph state.

## Verification Gate

Docs-only changes to this contract run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
go test ./internal/searchretrieval ./internal/searchdocs ./internal/searchbench \
  ./internal/searchnornicdb ./internal/searchpostgres ./internal/searchhybrid \
  ./internal/searchrerank \
  -count=1
git diff --check
```

Verification evidence must include a targeted sensitive-marker scan over every
changed public doc and navigation file.

Implementation PRs add:

- failing tests first for no-provider, policy-denied, index-unready, semantic
  active, hybrid degraded, and hybrid active paths
- API and MCP parity tests proving the same retrieval state and method
- a production-path drift guard proving configured API/MCP traffic uses the
  hybrid backend rather than bench-only wiring
- searchbench evidence comparing keyword baseline and semantic/hybrid candidate
- performance evidence for index build, query latency, corpus size, vector
  count, and memory/storage impact
- observability evidence for route span, retrieval method, index freshness,
  failure class, budget, and truncation signals
- security review for hosted provider, egress, credential handle, redaction,
  retention, and budget behavior

No-Regression Evidence: this contract is documentation-only. It changes no
embedder, provider, search index, query route, MCP tool, reducer, graph, queue,
CLI, or hosted runtime behavior.

No-Observability-Change: this contract adds no runtime behavior. Future
implementation PRs must name or add operator-visible status, log, metric, and
trace signals for semantic and hybrid retrieval.
