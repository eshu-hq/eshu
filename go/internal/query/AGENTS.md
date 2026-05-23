# AGENTS.md - internal/query

`internal/query` owns Eshu's HTTP read contracts, truth envelopes, capability
gates, OpenAPI fragments, and graph/content query ports. Treat every handler
change as a public wire-contract change.

## Read First

1. `contract.go` - `QueryProfile`, `GraphBackend`, truth levels, capability
   matrix, `BuildTruthEnvelope`, and profile gates.
2. `handler.go` - `APIRouter`, mount order, response helpers, and envelope
   negotiation.
3. `ports.go` - `GraphQuery` and `ContentStore` boundaries.
4. Matching `openapi_paths_*.go` file for any route you touch.
5. `go/internal/telemetry/contract.go` before adding or renaming spans/log keys.

## Mandatory Rules

- Capability-gate before any graph, content, or reducer-fact read. Use the
  existing profile gate and return `WriteContractError` for unsupported
  capability, never a partial answer.
- Every capability passed to `BuildTruthEnvelope` MUST exist in
  `capabilityMatrix`. Unknown capabilities are programmer errors and panic.
- Handlers MUST depend on ports, not drivers. Graph reads go through
  `GraphQuery`; content reads go through `ContentStore`; concrete adapters are
  wiring details.
- Use `WriteSuccess` for public success responses so MCP can negotiate
  `application/eshu.envelope+json`. Do not change `ResponseEnvelope`,
  `TruthEnvelope`, or `EnvelopeMIMEType` without architecture-owner approval
  and tracked compatibility evidence.
- A handler behavior change MUST update the matching OpenAPI fragment in the
  same PR. The OpenAPI spec is static Go text, not reflection.
- Do not add unauthenticated routes to `publicHTTPPaths` without explicit
  security review.

## Query Bounds

- Package-registry reads MUST require `limit` plus a route anchor such as
  `package_id`, `ecosystem`, `version_id`, or `repository_id`. Source hints are
  provenance, not ownership or runtime consumption truth.
- Entity-map reads MUST resolve one typed start entity before traversal. Default
  repository-anchor maps MUST use direct relationship-family traversal so
  structural edges do not expand before `limit` can bound the result.
- Dead-code scans MUST deduplicate entity IDs across candidate labels before
  hydration.
- Language-specific dead-code proof MUST keep the language filter so one
  language cannot fill the page before another is evaluated.
- JavaScript, JSX, TypeScript, and TSX dead-code candidates remain
  `ambiguous` until corpus precision evidence allows promotion.
- SQL routine reachability MUST keep the batched graph `EXECUTES` probe for
  `SqlFunction` candidates.
- Import dependency, call-graph, structural inventory, and no-cache prompt
  routes MUST require scope anchors, deterministic ordering, `limit+1`
  truncation probing, and negative-bound rejection.

## Change Routing

- New HTTP handler: add the handler struct, `Mount`, `APIRouter` field, router
  wiring in `cmd/api`, OpenAPI fragment, public docs, and tests.
- New capability: update `capabilityMatrix`, capability constants, YAML
  capability spec, handler truth envelope, public docs, and tests.
- Response shape change: update handler, OpenAPI fragment, public docs, MCP
  route expectations when exposed through tools, and tests.
- New graph query: keep backend-neutral query code where possible; add spans
  through existing query span helpers when it is a distinct user capability.

## Anti-Patterns

- Do not branch on graph backend in handlers. Backend-specific Cypher belongs in
  documented storage/cypher seams. The existing code-relationship NornicDB seam
  is the narrow exception.
- Do not import the Neo4j driver outside concrete query adapters and wiring.
- Do not use `panic` for profile-gate failures.
- Do not add whole-graph scans for prompt convenience.
- Do not return different JSON shapes between HTTP and MCP for the same route.

## Required Proof

- Run `go test ./internal/query ./cmd/api -count=1` for handler or OpenAPI
  changes.
- Run `go test ./internal/mcp -count=1` when a route is MCP-backed.
- Run `go run ./cmd/eshu docs verify ../go/internal/query --limit 1200 --fail-on contradicted,missing_evidence`
  for docs changes.
- Hot graph-query or unbounded-read risk also requires performance evidence and
  observability evidence in a tracked repo file.
