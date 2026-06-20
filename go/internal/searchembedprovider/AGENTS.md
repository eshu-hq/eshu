# AGENTS.md - internal/searchembedprovider guidance for LLM assistants

## Read first

1. `go/internal/searchembedprovider/README.md` - package purpose and boundary.
2. `go/internal/searchembedprovider/embedder.go` - provider-backed embedder.
3. `go/internal/semanticprofile/README.md` - provider profile and credential
   handle contract.
4. `docs/public/reference/hosted-search-embedder-gate.md` - approved hosted
   embedding posture.

## Invariants this package enforces

- Only profiles admitted for `search_documents` with source policy configured
  may construct an embedder.
- Do not use provider SDKs. Keep the transport plain `net/http` JSON.
- Do not return provider response bodies, raw source text, endpoint secrets, or
  credential material in errors.
- Search embeddings are derived retrieval features, not canonical graph truth.

## Common changes and how to scope them

- **Provider wire shape** - add a red test in `embedder_test.go` first and keep
  request/response structs private.
- **Credential support** - add focused tests for the credential source. Do not
  expose credential handles or values in returned errors.
- **Dimension behavior** - keep `Dimensions` fixed from the approved profile so
  vector metadata remains stable across builds and reads.

## Failure modes and how to debug

- Construction fails when the profile is missing the search-document source
  class, source policy, endpoint, model, dimensions, or resolvable credential.
- `Embed` returns a status-only provider error for non-2xx responses.
- Dimension mismatches are caller-visible as bounded validation errors before
  vector rows are persisted.

## Anti-patterns specific to this package

- Calling chat-completion adapters for embeddings.
- Persisting or logging raw provider responses.
- Adding request-time source policy bypasses.
- Importing this package from `searchhybrid` or `searchembed`.
