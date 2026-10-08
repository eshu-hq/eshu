# querycontract/repository

Read `go/internal/query/querycontract/AGENTS.md` first. The repository
contract rules here add to it; on a conflict the parent wins.

- This leaf owns the repository read models, the summary/relationship
  loaders, and the repository row projection. It owns no handler,
  route, store implementation, or Cypher query text.
- Import one way only: this leaf imports `querycontract` for the
  `ContentStore` port and the row-shaping helpers. The parent never
  imports this leaf. Nothing here may import package `query`, a handler
  family, a graph driver, or Postgres.
- `RepoProjection` returns a RETURN-list fragment the caller splices
  into its own query, the same carve-out as the authorization seam's
  WHERE fragments. Never grow it into a complete query.
- The `Available` flag on the summary type is load-bearing: false means
  the read model holds nothing, and the caller must fall back to the
  graph counts. Keep that contract or update every caller with it.
- Callers that import both this leaf and the repository handler family
  spell this leaf `repositorycontract`. Keep that spelling; do not
  invent a second alias.
- Verify with `go test ./internal/query/querycontract/...` from `go/`.
