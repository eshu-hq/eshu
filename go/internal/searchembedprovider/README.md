# Search Embed Provider

## Purpose

`searchembedprovider` owns governed hosted embedding adapters for curated
search documents. It lets reducer vector builds and API/MCP query embedding use
the same approved provider profile without placing hosted traffic inside the
local-only `searchembed` or pure retrieval `searchhybrid` packages.

## Ownership boundary

This package owns provider-profile admission, credential resolution for the
supported runtime credential sources, `/v1/embeddings` JSON transport, response
validation, and leak-free provider error handling. It does not own source
policy parsing, search-document projection, vector persistence, API/MCP routing,
canonical graph truth, or ANN retrieval.

## Exported surface

- `Embedder` - provider-backed implementation of the `searchhybrid.Embedder`
  port.
- `New` - validates a semantic provider profile and constructs an embedder.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/semanticprofile` - provider kind, credential source, source
  class, and profile metadata.

## Telemetry

This package emits no metrics or spans directly. Callers diagnose provider
usage through the existing search-vector build logs/results, Postgres vector
metadata/value read spans, semantic-search route spans, and status surfaces.

## Gotchas / invariants

- Profiles must include `search_documents`, have source policy configured, name
  a positive embedding dimension, and provide an endpoint profile URL.
- Environment-variable credential sources read only the value named by the
  profile handle. The handle itself stays out of public status and errors.
- Provider response bodies are always drained and discarded before errors are
  returned.
- Embeddings are derived retrieval features and must never create canonical
  graph truth.

## Related docs

- `docs/public/reference/hosted-search-embedder-gate.md`
- `docs/public/reference/semantic-hybrid-search-admission.md`
- `docs/public/reference/search-retrieval-contract.md`
