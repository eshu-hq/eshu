# AGENTS.md - internal/searchembed guidance for LLM assistants

## Read first

1. `go/internal/searchembed/README.md` - package purpose and no-network boundary.
2. `go/internal/searchembed/hash.go` - local embedder implementation.
3. `go/internal/searchhybrid/README.md` - retrieval backend and `Embedder` port.
4. `docs/public/reference/semantic-hybrid-search-admission.md` - production
   admission and truth boundaries.

## Invariants this package enforces

- Embedders here are deterministic for the same input and configuration.
- This package must not import hosted SDKs, call HTTP, read credentials, or log
  raw source text.
- Embedding vectors are derived retrieval features, never canonical graph truth.

## Common changes and how to scope them

- **Change token handling** - add or update focused tests in `hash_test.go`
  first. Keep normalization aligned with `searchhybrid.QueryTerms`.
- **Change vector dimensions** - preserve constructor validation and deterministic
  output across process runs.
- **Add a provider-backed embedder** - do not add it here without an approved
  hosted-provider design and security review; this package is the local boundary.

## Failure modes and how to debug

- Symptom: zero semantic scores - confirm input has normalized query terms and
  the configured dimensions are positive.
- Symptom: unstable rankings - check for map iteration or non-deterministic
  hashing before changing `searchhybrid` ranking.
- Symptom: unexpected production claim - verify the caller is not treating this
  local feature hash as ANN or hosted-model readiness.

## Anti-patterns specific to this package

- Calling external services or reading provider credentials.
- Persisting raw source text, secrets, or token-bearing provider configuration.
- Promoting vector scores into graph truth or relationship confidence.
