# Reducer contract

## Purpose

`contract` holds the reducer types that domain-family packages need to declare
and handle work without importing the parent `internal/reducer` package.

## Ownership boundary

This package owns the `Domain` catalog and validation set, durable `Intent` and
`Result` values, handler interfaces, ownership validation, and domain
definitions. The parent reducer package owns registry composition, runtime and
queue execution, adapters, retries, telemetry, and graph writes.

## Exported surface

The exported surface is `Domain`, the reducer and shared-projection domain
constants, `KnownDomains`, `ParseDomain`, lifecycle statuses, `FailureRecord`,
`RetryableError`, `Intent`, `Result`,
`OwnershipShape`, `CrossScopeDependency`, `DomainDefinition`, `Handler`, and
`HandlerFunc`. See [doc.go](doc.go) for the package contract.

## Dependencies

The package imports only the standard library and `internal/truth`. It does not
import the parent reducer package or any storage, queue, graph, telemetry, or
domain-family implementation.

## Telemetry

This package emits no telemetry. The parent reducer runtime records execution
spans, counters, and structured logs around contract handlers.

## Gotchas / invariants

- `KnownDomains` contains 69 validation identifiers. Three are reserved and
  non-registrable, leaving 66 production-registrable domains.
- `Intent.Clone` detaches slices, the top-level payload map, timestamps, and the
  failure record. Values nested inside the payload remain shared and must be
  treated as immutable.
- A valid ownership shape is cross-source and cross-scope and declares either a
  canonical write or bounded counter emission.
- The parent package aliases these types; changing a field or method changes the
  existing `reducer` API too.

No-Regression Evidence: #6100 moves the existing value types and validation
methods without changing fields, constants, lifecycle rules, or registry order.
`go test ./internal/reducer/... -count=1` covers the root compatibility aliases,
the exact 66-domain registrable set, the 14-entry base catalog order, the exact
83-domain root `AllDomains` union order, and the existing reducer behavior. A
scratch move of the security-alert domain-definition family into
`internal/reducer/securityalert` imported only this package and `internal/truth`;
`go test ./internal/reducer/... -count=1`, `go build ./...`, and `go vet ./...`
all exited 0 before the scratch move was removed from the final diff. This
proves the domain-definition registration seam. The full security-alert handler
still depends on root-owned fact-loading, quarantine, telemetry, and writer
helpers; those are deliberately outside this neutral registry contract and
remain work for the family-move issue. The final diff then passed the same
whole-module build and vet plus the focused changed-package lint.

No-Observability-Change: #6100 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, log
field, or status surface. The parent runtime still wraps the same aliased
handlers with its existing telemetry.

No-Regression Evidence: #6061 moves the two generation-check func types
(`GenerationFreshnessCheck`, `PriorGenerationCheck`) out of the reducer root into
this package and leaves type aliases behind, so every one of the 92 root callers,
plus `cmd/reducer` and `internal/storage/postgres`, compiles against the identical
type. A Go type alias is the same type, not a conversion or a wrapper, so there is
no new indirection on any call path and nothing to measure: the declarations carry
no function bodies at all. Baseline `origin/main` at `0a0700ab1` and the branch
build the same binaries; `go build ./...`, `go vet ./...` (which also compiles
test files), `go test ./internal/reducer/... -count=1` (15 packages), and
`go test ./cmd/reducer ./internal/storage/postgres -count=1` each exited 0.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, log
field, or status surface. The types name a callback shape that the runtime already
invoked; the handlers holding those callbacks and their telemetry are untouched.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
