# Impact handler family

## Purpose

`impact` holds the ImpactHandler HTTP surface and every file that declares
one of its methods (Issue #6060, lane B): blast radius, change surface
(investigate, legacy, traversal), pre-change checks, developer change plans,
contracts, entity maps, resource investigation, deployment-trace chain and
its GitOps/OCI/K8s/source pieces, exposure paths, and the exported seam the
staying root package consumes through aliases in `family_impact_shim.go`.

## Ownership boundary

This package owns handler orchestration for the impact routes and the
`ImpactHandler` struct with its `Neo4j`, `Content`, `Profile`, `TraceContext`,
`CodeSurface`, and `PathProbe` dependencies. Non-method helpers the family
needs but that touch no handler state live in `impacttrace`; this package
imports `impacttrace`, never the reverse, and neither imports the query
root (the root would cycle back through `compare.go` and
`family_impact_shim.go`).

It also owns the family's capability rows (`capabilities.go`, registered via
`querycontract.RegisterCapabilities`): each family declares the support
contract for the routes it implements, following the
`semanticsearch.Support` precedent. The query root's matrix must not repeat
these rows — duplicate initialization is a contract failure.

The staying root package keeps thin aliases and forwarders
(`family_impact_shim.go`) so external callers (`query_test` seam tripwires,
`cmd` wiring) keep their spelling. Production graph drivers, the Neo4j
driver, service-story shaping, and entity handling stay in root behind the
`TraceContext`, `CodeSurface`, and `PathProbe` interfaces; the root `init`
assigns the production adapters.

## Exported surface

The exported surface is described in [doc.go](doc.go). Method families that
moved here keep an exported boundary for the staying root tests that pin
them (query text builders, selector predicates, traversal specs, result
types with exported `Rows`/`Limits` fields or `Rows()`/`Limits()` methods,
the `entityMapResolverQuery` `Cypher`/`Params` fields). Unexported helpers
stay unexported; cross-package test pins go through `querytestutil`
(`FakeGraphReader`, `FakePortContentStore`, `RecordingResourceInvestigationGraph`,
`SqlBlastRadius*` cleanup probes, `ScopedTestAuthContext`) or `querycontract`
(row-value decoders, shared bounds, ports).

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
capability registry, shared bounds), `querytestutil` in tests only,
`impacttrace`, `queryauth` (tests), and `queryspan`/`internal/telemetry`
for handler spans. It must not import the query root or graph drivers.

## Telemetry

Handlers keep their existing `query.*` spans and `eshu_dp_api_request_duration_seconds`
timing; the move changes no operator signal. New runtime behavior must add
spans an operator can use at 3 AM.

No-Observability-Change: relocating handler methods between packages emits
nothing and moves no span boundary, attribute, or log line.

## Performance

Handler methods are request-orchestration, not a hot decode loop; the move
adds no call indirection to row paths (shared helpers are called directly,
and the small `querycontract` row decoders inline away). No benchmark delta
is claimed because there is no runtime delta to measure.

No-Regression Evidence: `go test ./internal/query/... ./internal/mcp/... -count=1`,
`go test ./internal/queryplan/ -count=1`, the golden-corpus, replay-coverage,
and ci gates, plus `go build ./...` and `go vet ./...`, all exit 0. The B-7
cassettes and B-12 snapshot are byte-identical: the diff moves definitions,
import blocks, and manifest digests, and touches no Cypher text, queue,
lease, or projection path.

## Gotchas / invariants

- A test binary for this package does not run the query root's `init`, so
  the capability registry and the `Default*` backends arrive empty. Gated
  HTTP paths answer 501 and backend-backed reads nil-panic. The external
  `impact_test` init file (`impact_defaults_test.go`) wires the production
  adapters from root constructors — legal because nothing imports
  `impact_test`, so it cannot cycle — reproducing the base environment
  exactly. Tests needing narrower behavior inject per-handler fakes instead.
- `BuildDeploymentTraceResponse` takes a caller-built, non-nil overview map
  and attaches counts in place. Counts that are pure functions of the fields
  (instance/environment/platform/config counts, evidence counts) derive
  inside, reproducing the service-story builder's values exactly; evidence
  lists attach only when the caller left them absent, so the builder's
  normalized forms win in production. `deployment_truth_tier` stays fully
  caller-owned.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext`, never hand-built to widen access.

## Verification

From `go/`, run `go test ./internal/query/... ./internal/mcp/... -count=1`,
`go test ./internal/queryplan/ -count=1`, `go build ./...`, and
`go vet ./...`. From the repository root, run
`scripts/verify-package-docs.sh` and the B-7 golden-corpus proof selected by
the parent package instructions.

## Related docs

- [Source layout](../../../../docs/public/reference/source-layout.md)
- [HTTP API](../../../../docs/public/reference/http-api.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
