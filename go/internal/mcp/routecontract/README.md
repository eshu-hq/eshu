# MCP route contract

## Purpose

`routecontract` holds the values a domain router needs to select an internal
HTTP request without importing the parent MCP package.

## Ownership boundary

This package owns decoded argument access and the selected request value. It
does not own tool names, family membership, or route-selection policy. Family
packages such as `internal/mcp/admissiondecisions`, `internal/mcp/ask`,
`internal/mcp/cicd`, `internal/mcp/codeowners`, `internal/mcp/containerimage`,
`internal/mcp/kubernetes`, `internal/mcp/observabilitycoverage`,
`internal/mcp/packageregistry`,
`internal/mcp/relationships`, `internal/mcp/secretsiam`,
`internal/mcp/securityalert`, `internal/mcp/supplychainimpact`, and
`internal/mcp/visualization` own those decisions. The root `internal/mcp`
package still owns global route fanout, family adapters, request dispatch,
authorization forwarding, timeouts, response budgets, response envelopes,
transport behavior, and telemetry.

## Exported surface

- `Arguments` is the decoded MCP argument map. Its methods preserve the root
  dispatcher's existing type coercions and defaults.
- `Request` carries the internal HTTP method, path, body, and query.

See `doc.go` for the godoc contract.

## Dependencies

None. The package uses only Go built-in types so child route packages can
import it without creating a parent-child cycle.

## Telemetry

None. The root MCP dispatcher and the selected HTTP query handler retain their
existing logs, spans, and metrics.

## Gotchas / invariants

- `IntOr` accepts `int`, `int64`, and `float64`; other numeric types use the
  caller's fallback.
- `OptionalFloat` accepts `float64`, `float32`, `int`, and `int64`.
- `StringSlice` returns an existing `[]any` directly and converts `[]string` to
  `[]any`. Other input shapes return nil.
- `Request` describes a route. It does not execute one.

No-Observability-Change: extracted family routes still run through the root MCP
dispatcher and the same HTTP query handlers, which retain the existing MCP
transport and API request telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP admission-decisions route selection](../admissiondecisions/README.md)
- [MCP Ask registration and route selection](../ask/README.md)
- [MCP CI/CD run-correlation route selection](../cicd/README.md)
- [MCP CODEOWNERS ownership route selection](../codeowners/README.md)
- [MCP container-image identity route selection](../containerimage/README.md)
- [MCP Kubernetes-correlation route selection](../kubernetes/README.md)
- [MCP observability-coverage route selection](../observabilitycoverage/README.md)
- [MCP package-registry route selection](../packageregistry/README.md)
- [MCP relationship registrations](../relationships/README.md)
- [MCP secrets/IAM route selection](../secretsiam/README.md)
- [MCP security-alert reconciliation route selection](../securityalert/README.md)
- [MCP supply-chain-impact route selection](../supplychainimpact/README.md)
- [MCP visualization registration and route selection](../visualization/README.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)

## Verification

From `go/`, run `go test ./internal/mcp/routecontract ./internal/mcp -count=1`
and `go vet ./internal/mcp/routecontract ./internal/mcp`.
