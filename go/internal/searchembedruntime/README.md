# Search Embed Runtime

## Purpose

`searchembedruntime` selects the embedder identity shared by reducer vector
builds and API/MCP semantic search reads. It prevents the three runtimes from
drifting on provider profile id, model id, vector index version, source class,
and retrieval mode.

## Ownership boundary

This package owns environment-driven embedder selection only. It does not own
provider HTTP transport, semantic policy parsing, search-document projection,
Postgres vector storage, query handlers, MCP tools, or graph truth.

## Exported surface

- `Config` - selected embedder, persisted-vector identity, and retrieval mode.
- `ConfigFromEnv` - loads explicit local hash mode or a governed provider
  profile from runtime environment variables.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/searchembed` - deterministic zero-key local hash embedder.
- `go/internal/searchembedprovider` - governed hosted `/v1/embeddings` adapter.
- `go/internal/semanticpolicy` - source-policy and semantic-provider egress
  intersection for governed search profiles.
- `go/internal/semanticprofile` - provider profile parsing and source-class
  constants.
- `go/internal/searchhybrid` - `Embedder` port and vector retrieval mode.

## Telemetry

This package emits no metrics or spans. API, MCP, reducer, and Postgres stores
emit the runtime signals for search-vector build and retrieval behavior.

## Gotchas / invariants

- `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` or `local_hash` always selects the
  deterministic no-network embedder.
- With no local override, exactly one governed `search_documents` provider
  profile may become default-on only after the semantic policy and egress
  allowlist admit that profile/source-class pair.
- Multiple governed provider profiles require
  `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` to avoid silent profile drift.
- The selected identity must be used for both vector builds and query-time
  vector reads.
- Provider-backed reducer builds must call `AllowsSearchDocument` for each
  curated document before embedding so repository scope and source allowlist
  rules are enforced at dispatch time, not only at startup.

## Related docs

- `docs/public/reference/environment-runtime-storage.md`
- `docs/public/reference/semantic-hybrid-search-admission.md`
- `docs/public/reference/hosted-search-embedder-gate.md`
