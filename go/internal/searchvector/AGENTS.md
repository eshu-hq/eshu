# AGENTS.md - internal/searchvector guidance for LLM assistants

## Read first

1. `go/internal/searchvector/README.md` - package purpose and no-network
   vector-build boundary.
2. `go/internal/searchvector/builder.go` - replayable local vector build
   contract.
3. `go/internal/searchhybrid/README.md` - document text, content hash, and
   `Embedder` port contract.
4. `go/internal/storage/postgres/README.md` - vector metadata/value store
   ownership and active-generation read semantics.
5. `docs/internal/design/2578-ann-vector-index-production-hybrid-search-contract.md`
   - production vector-lane storage contract.

## Invariants this package enforces

- Build from active curated search documents only; do not scan raw facts or the
  canonical graph here.
- Use deterministic, no-network embedders through `searchhybrid.Embedder`.
- Persist vector metadata and values as derived read-model state only.
- Keep API, MCP, OpenAPI, graph writes, reducer workers, queues, and hosted
  provider behavior out of this package.

## Common changes and how to scope them

- **Change build identity** - update tests first. Keep idempotency keyed by
  scope, generation, document id, embedding model id, and vector index version.
- **Change failure handling** - add a red test proving the bounded failure class
  and metadata row shape. Do not persist raw input text or provider error text.
- **Add worker or queue wiring** - use `concurrency-deadlock-rigor` and keep it
  out of this package unless a new issue explicitly owns scheduling.

## Anti-patterns specific to this package

- Calling hosted embedding APIs or reading credentials.
- Writing graph state or treating vector similarity as canonical truth.
- Reconstructing a separate search text contract instead of using
  `searchhybrid.DocumentText` and `searchhybrid.DocumentContentHash`.
