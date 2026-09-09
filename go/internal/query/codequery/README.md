# Code Query Family (`go/internal/query/codequery`)

The code-family query handlers: `*CodeHandler`, its `Mount` route table
for `/api/v0/code/*`, and the NornicDB/postgres readers behind search,
symbols, structural inventory, topics, secrets, imports, call graph,
flow, relationships, dead code, complexity, quality, call chain,
route-to-caller, cypher, visualize, and bundles.

It moved out of root package `query` (#6060 lane A) so the family can be
read, tested, and changed without pulling in the rest of the query
surface. Exactly 40 non-test `.go` files (dirgate cap).

## How it connects

- Root keeps `type CodeHandler = codequery.CodeHandler`
  (`go/internal/query/code_alias.go`) and the seam
  (`go/internal/query/code_seam.go`) for staying callers: content
  readers, the contract capability matrix, and both cmd wirings.
- `codemodel` (sibling leaf) owns the read-model builders, row
  decoders, and response shapers this family's handlers execute.
- Language-specific queries are NOT here: `LanguageQueryHandler`
  stays in root (this package cannot import root back without a
  cycle) and mounts from `APIRouter.Mount` via its own `Language`
  field. Both cmd wirings construct it with
  Neo4j/Content/Profile/Logger.
- Tests that pin production Cypher bytes drive handlers over HTTP
  (`httptest` + `handleSearch`-style direct handler calls), never
  over internal helpers: `handleSearch` probes with limit+1, so a
  body limit of 9 drives the graph builder with 10.

## Contracts (must hold)

- **Never import root package `query`.** Root imports this package;
  the reverse is an import cycle. Qualify to the leaves instead.
- **Relocated, not rewritten.** Queryplan-pinned builders keep their
  emitted Cypher byte-identical. Where the move forced a spelling
  change (a seam export, a leaf qualification), a same-named alias
  (`relationshipStoryRequest`, `callGraphMetricsEdgeScanLimit`)
  preserves the declaration bytes so `source_sha256` keeps matching.
  Never re-freeze a `cypher_sha256`.
- **Moving a builder moves its pin.** Update the `file:` path (and
  the `source_sha256` only when the declaration bytes changed for a
  sanctioned reason) in
  `go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
  and re-run `go test ./internal/queryplan/`.
- **Live proofs stay honest.** `live_nornicdb_reader_test.go` is the
  test-only live-backend reader (no production read policy); the
  `live_nornicdb_*` tag-gated files prove query text against a real
  backend, not the policy layer.

## Verification

```bash
cd go && go build ./internal/query/...
cd go && go vet -gcflags=-e ./internal/query/...
cd go && go test ./internal/query/ ./internal/query/codequery/ -count=1
cd go && go test ./internal/queryplan/ -count=1
cd go && go test ./internal/query/ -run 'QueryplanManifestBindsProduction' -count=1
```
