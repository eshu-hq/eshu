# internal/query

`internal/query` owns public HTTP read contracts, truth envelopes, capability
gates, OpenAPI fragments, and graph/content query ports. Treat handler changes
as public wire-contract changes.

## Read First

1. `go/internal/query/README.md`
2. `go/internal/query/doc.go`
3. `go/internal/query/contract.go`
4. `go/internal/query/handler.go`
5. `go/internal/query/ports.go`
6. The matching `openapi_paths_*.go` file for any touched route
7. `go/internal/telemetry/contract.go` before adding or renaming telemetry

## Package Rules

- Use `eshu-mcp-call-rigor` for MCP/API tool contracts and bounded
  graph-backed read design. Add `cypher-query-rigor` for Cypher or graph query
  shape changes.
- Capability-gate before graph, content, or reducer-fact reads. Unsupported
  capabilities MUST return `WriteContractError`, not partial answers.
- Every capability passed to `BuildTruthEnvelope` MUST exist in
  `capabilityMatrix`; unknown capabilities are programmer errors.
- Handlers MUST depend on `GraphQuery`, `ContentStore`, or query-local read
  ports. Do not import graph or SQL drivers in handlers.
- Public success responses MUST go through `WriteSuccess` so HTTP and MCP share
  the canonical envelope and `application/eshu.envelope+json` negotiation.
- `ResponseEnvelope`, `TruthEnvelope`, `EnvelopeMIMEType`, route behavior,
  OpenAPI fragments, public docs, MCP expectations, and tests MUST move
  together.
- Adding unauthenticated paths to `publicHTTPPaths` requires explicit security
  review.
- Backend-specific Cypher belongs in documented adapter seams, not handlers.

## Query Bounds

- List-style reads MUST require scope anchors, deterministic ordering, `limit+1`
  truncation probing, negative-bound rejection, and a `truncated` signal.
- Package-registry reads MUST require `limit` plus an ownership anchor such as
  `package_id`, `ecosystem`, `version_id`, or `repository_id`.
- Entity-map reads MUST resolve one typed start entity before traversal.
- Dead-code reads MUST deduplicate entity IDs before hydration and preserve
  language maturity/exactness blockers. JS, JSX, TS, and TSX candidates remain
  `ambiguous` until corpus precision evidence supports promotion.
- SQL routine reachability MUST keep the batched graph `EXECUTES` probe for
  `SqlFunction` candidates.
- Do not add whole-graph scans or prompt-convenience routes without a bounded
  contract, telemetry, and performance evidence.

## Proof

- Run `cd go && go test ./internal/query ./cmd/api -count=1` for handler,
  OpenAPI, envelope, or capability changes.
- Run `cd go && go test ./internal/mcp -count=1` when a route is MCP-backed.
- Run `go run ./cmd/eshu docs verify ../go/internal/query --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
- Hot graph-query or unbounded-read risk also requires tracked performance and
  observability evidence.
