# Collector observability contracts

## Purpose

`collector/observability` groups the collector-side source observers that
read live observability-provider metadata for freshness and drift evidence.
It is a documentation-only namespace; implementation belongs in its leaf
packages and collection behavior stays in the owning collector packages.

## Ownership boundary

The namespace owns no runtime declarations, provider access, fact emission,
storage calls, graph writes, or telemetry. Its `tempo` child owns only
metadata-only collection of Tempo trace-signal metadata: source instances,
tag names, bounded tag values, and coverage warnings. It does not fetch
traces, spans, raw trace IDs, request attributes, TraceQL bodies, or trace
search payloads, and it makes no graph or coverage truth decisions.
Reducers and query surfaces compare these facts with declared and applied
evidence.

## Exported surface

None. This parent is documentation-only. See the `tempo` child for
`NewHTTPClient`, `HTTPClient.CollectObservedMetadata`, `NewClaimedSource`,
and the source-instance, observed-trace-signal, and coverage-warning
envelope constructors.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages. The `tempo` child retains its
existing dependency on the collector root (`FactsFromSlice` and
claim-source generation output) plus `facts`, `scope`, `telemetry`,
`workflow`, `collector/sdk`, and the SDK factschema contracts; a future
extraction of this subtree must address that coupling first.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract. The `tempo` child's spans and counters are documented in
`go/internal/collector/observability/tempo/README.md` and covered by the
existing collector Tempo telemetry row.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the owning collector.
- Keep provider access, fact emission, ACL handling, and telemetry behavior
  in the owning collector slice.
- The `tempo` leaf is an observability source boundary, not an independently
  deployable service.
- Observed Tempo tags are fallback, validation, drift, and freshness
  evidence after IaC-first declared/applied sources.

## Related docs

- `go/internal/collector/observability/tempo/README.md`
- `go/internal/collector/README.md`
- `docs/public/reference/observability-evidence.md`
