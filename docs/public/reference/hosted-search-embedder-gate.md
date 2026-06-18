# Hosted Search Embedder Gate

This gate defines the security and schema review required before Eshu may send
curated search-document text to a hosted embedding provider or internal
gateway. It documents the approval boundary for #3047 and blocks #3042 until a
separate approval record names the accepted implementation details.

This page does not add provider traffic, runtime flags, credential loading,
provider SDKs, API fields, MCP fields, reducer behavior, queue behavior, graph
writes, or vector storage changes.

## Current State

The default semantic and hybrid search path remains deterministic and
no-network:

- `go/internal/searchhybrid` owns bounded BM25/vector fusion and keeps hosted
  adapters out of that package.
- `go/internal/searchembed` owns the local feature-hash embedder.
- API and MCP can opt into `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` or
  `local_hash` only when compatible persisted local vectors are ready.
- Provider profiles and semantic egress policy are parsed for governed semantic
  extraction status, but they do not yet approve a concrete search-embedding
  provider client.

## Review Questions

Security and schema review must answer these questions before implementation:

1. Which source class admits curated `EshuSearchDocument` text for search
   embedding builds?
2. Which package owns hosted search-embedding adapters outside
   `go/internal/searchhybrid`?
3. Which credential-handle and endpoint-profile shapes are allowed?
4. Which source identifiers, graph handles, paths, labels, and text fields may
   enter the embedding request after redaction?
5. Which provider response fields may be retained, hashed, counted, or discarded?
6. Which vector metadata must be persisted for active-generation compatibility,
   rollback, retry, and stale-index diagnosis?
7. Which timeout, retry, concurrency, token, byte, and cost budgets apply per
   scope and provider profile?
8. Which failure classes are operator-visible without exposing source text,
   provider response bodies, raw endpoints, credentials, or private deployment
   details?

## Request Contract

A hosted search-embedding request may be created only after all admission gates
pass:

| Field | Requirement |
| --- | --- |
| Scope | Repository or narrower canonical scope, with ACL state already admitted. |
| Source class | Approved by the #3047 review and present in provider profile and policy. |
| Source selector | Policy allowlist must match the curated document source. |
| Text | Redacted, bounded search text only; no raw provider payloads or graph dumps. |
| Credential | Handle only; never a raw key, token, or credential-bearing URL. |
| Endpoint | Endpoint profile id only; never a token-bearing URL in public status. |
| Budget | Positive byte, token, cost, timeout, and concurrency limits. |
| Retention | Metadata-only by default, with prompt/input body retention disabled. |

The request must fail closed before provider dispatch when profile, policy,
egress, ACL, redaction, budget, retention, or scope checks fail.

## Response Contract

A hosted search-embedding response may persist only bounded derived state:

- embedding vector with dimension and vector schema version;
- provider profile id or local profile class;
- model id or version;
- source class and scope;
- content hash and active generation id;
- redaction policy version;
- vector index version;
- failure class, retryability, and build timestamp.

It must not persist or expose raw provider responses, raw prompt/input text,
source identifiers outside approved private stores, raw endpoint URLs,
credential handles in public payloads, provider error bodies, or high-cardinality
metric labels.

## Failure Classes

Hosted search embedding implementation must use bounded failure classes that
align with the semantic search admission contract:

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

Provider-specific error strings belong only in redacted private diagnostics when
explicitly approved. Public status, logs, metrics, docs, PRs, and issue comments
must use bounded classes.

## Observability Gate

Implementation must give operators enough signal to diagnose the path without
exposing sensitive material:

- build attempts by state, source class, provider kind, and provider profile
  class;
- provider dispatch duration and timeout counts;
- policy, egress, ACL, redaction, budget, and retention denial counts;
- active vector row counts, stale counts, failed counts, and schema mismatch
  counts;
- route-level retrieval state, method, index freshness, truncation, and failure
  class.

Metric labels must stay low-cardinality. Raw document ids, source ids, paths,
query text, prompt/input bodies, provider response bodies, endpoints, and
credentials must not be metric labels.

## Approval Gate

#3042 may implement hosted search embeddings only after the owning reviewers add
a concrete approval record to #3047 or an explicitly linked security/schema
review. Closing #3047 by adding this gate is not sufficient. The approval record
must name:

- the accepted source class;
- the adapter package boundary outside `go/internal/searchhybrid`;
- the request and response schema;
- the credential-handle and endpoint-profile rules;
- the redaction and retention posture;
- the timeout, retry, budget, and concurrency envelope;
- the vector metadata required for active-generation compatibility;
- the operator-facing telemetry and status contract.

Until that approval record exists, safe repo-local work is limited to
documentation, fail-closed tests, and status contracts that perform no outbound
provider traffic.

## Verification

Docs-only changes to this gate run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Implementation PRs must add focused tests for provider-not-configured,
policy-denied, ACL-denied, redaction-failed, budget-exhausted, provider
unavailable, dimension mismatch, timeout, stale vectors, failed vectors, and
successful active retrieval.

No-Regression Evidence: this gate is documentation-only. It changes no
embedder, provider client, search index, query route, MCP tool, reducer, graph,
queue, CLI, Helm setting, or hosted runtime behavior.

No-Observability-Change: this gate adds no runtime behavior. Future
implementation PRs must add or name operator-visible metrics, spans, logs, and
status fields before enabling hosted search-embedding traffic.
