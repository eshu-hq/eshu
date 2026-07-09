# ifa/graphdump

## Purpose

`graphdump` canonicalizes an arbitrary property graph (any label set, any
edge type) into a stable, content-addressed byte form so two reads of a graph
can be compared for exact equality. It is the graph-truth half of Ifá's P3
determinism matrix (issue [#4396](https://github.com/eshu-hq/eshu/issues/4396),
design doc `docs/internal/design/4389-ifa-conformance-platform.md`, Layer 2):
after replaying the same Odù at worker counts N ∈ {1, 2, 4, ...}, a follow-on
slice's determinism matrix canonicalizes the resulting graph at each N and
asserts the bytes are identical, so a divergence is a real concurrency defect
(a MERGE race, a dropped write, an ordering-dependent projection) rather than
a scan-order or backend-ID artifact.

See `doc.go` for the full design rationale (content addressing vs. ID
addressing, the reused canonical JSON core, and the over-normalize /
under-normalize tradeoff behind `normalize.go`'s denylist).

## Ownership Boundary

This package owns canonicalization logic only: `Node`, `Edge`, `Reader`,
`Canonicalize`, `Digest`, and `Equal`. It is deliberately driver-free — it has
no NornicDB/Neo4j/Bolt dependency and no test requires Docker or a live graph
— so its logic is provable against the in-memory `fakeReader` every test in
`graphdump_test.go` uses.

A live, Bolt-backed `Reader` implementation is out of this package's scope by
design: `go/cmd/ifa`'s `ifa graph-dump` verb (`graphdump_reader.go`,
`graph_dump.go`) implements `Reader` over a real NornicDB/Neo4j session and is
the only production caller. Keeping the Bolt dependency in `cmd/ifa` rather
than here means a change to the driver, the connection lifecycle, or the
`runtime.OpenNeo4jDriver` env contract can never silently change this
package's hermetic test guarantee.

## Exported Surface

- `Node{Labels []string, Props map[string]any}` - a node's canonicalizable
  identity; no internal element ID.
- `Edge{Type string, FromLabels/FromProps, ToLabels/ToProps, Props}` - a
  relationship's canonicalizable identity; endpoints are repeated by
  labels+props, never referenced by index or backend ID.
- `Reader` - the narrow `Nodes(ctx)`/`Edges(ctx)` read seam `Canonicalize`
  needs; production callers implement it over a live Cypher session (see
  `go/cmd/ifa/graphdump_reader.go`), tests implement it over a plain slice.
- `Canonicalize(ctx, Reader) ([]byte, error)` - returns the graph's stable
  canonical byte form: content-addressed, order-independent, and idempotent.
- `Digest(ctx, Reader) (string, error)` - the sha256 hex digest of
  `Canonicalize`'s output.
- `Equal(ctx, a, b Reader) (bool, error)` - convenience wrapper comparing two
  Readers' digests.

## Dependencies

`go/internal/replay` for the shared canonical JSON core
(`CanonicalizeValue`/`CanonicalOptions`) — see `doc.go`'s "Reused canonical
JSON core" section for why this package does not implement a second
canonicalizer. No other internal or external dependency.

## Performance and observability evidence

(The marker lines below carry a trailing colon on purpose: the
`verify-performance-evidence.sh` hot-path gate matches `Performance Evidence:`
/ `Benchmark Evidence:` / `No-Regression Evidence:` and `Observability
Evidence:` / `No-Observability-Change:`.)

- No-Regression Evidence: `Canonicalize`/`Digest`/`Equal` are reached only
  from `ifa graph-dump` (a credential-free, read-only local diagnostic verb —
  see `go/cmd/ifa/graph_dump.go`) and this package's own tests; no production
  ingester, reducer, API, or MCP path calls them, so no existing hot path
  changes behavior or timing. `graphdump_reader.go`'s Bolt-backed `Reader`
  (`boltGraphReader`) issues two plain, unbounded `MATCH` reads
  (`MATCH (n) RETURN labels(n), properties(n)` and the one-hop edge
  equivalent) against the graph backend and performs no write of any kind;
  `neo4j.ExecuteQuery`'s default routing (`RoutingControl = Write`, the same
  default `cmd/golden-corpus-gate/graph.go`'s `boltGraphCounter` uses
  unchanged) sends the read to the same instance a write would, so this verb
  adds no new read-replica routing behavior either. This gate-worthy Cypher
  surface has no prior baseline to regress against: it is new, additive, and
  off the ingest/reducer/query hot path entirely.
- No-Observability-Change: this slice mints no new metric instrument, span,
  or dashboard. `runGraphDumpCommand` returns a plain error and writes its
  canonical bytes/digest to stdout or `-out`; there is no `log/slog` logging,
  no `telemetry.Instruments` field, and no operator-facing counter. Operator
  visibility for this verb is its own CLI output (canonical bytes or a
  digest), not a runtime signal — it has no runtime deployment to observe.

## Verification

```bash
cd go && go test ./internal/ifa/graphdump/... -count=1
cd go && go test -race ./internal/ifa/graphdump/... ./cmd/ifa/... -count=1
```

## Related Docs

- `doc.go` - full design rationale.
- `go/cmd/ifa/README.md` - the `ifa graph-dump` verb this package's `Reader`
  is implemented for.
- `docs/internal/design/4389-ifa-conformance-platform.md` - the ADR (Layer 2,
  P3 determinism matrix).
