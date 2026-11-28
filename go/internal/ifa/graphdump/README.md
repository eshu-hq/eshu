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
- `Reader` - the narrow streaming read seam `Canonicalize` needs:
  `StreamNodes(ctx, yield)` / `StreamEdges(ctx, yield)`. Production callers
  implement it over a live Cypher session yielding straight off the Bolt cursor
  (see `go/cmd/ifa/graphdump_reader.go`); tests implement it over a plain slice.
- `Canonicalize(ctx, Reader) ([]byte, error)` - returns the graph's stable
  canonical byte form: content-addressed, order-independent, and idempotent.
- `Digest(ctx, Reader) (string, error)` - the sha256 hex digest of
  `Canonicalize`'s output.
- `Equal(ctx, a, b Reader) (bool, error)` - convenience wrapper comparing two
  Readers' digests.

## Memory (issue #5009)

`Canonicalize` streams: each node/edge is canonicalized to bytes inside the
reader's `yield` callback and the struct is then discarded, so peak memory holds
the canonical record set (`[][]byte`), not the full `Node`/`Edge` struct graph —
which is far larger, because every `Edge` duplicates both endpoints' property
maps. The final document is assembled directly from the sorted record bytes
(re-indented to their nested position), avoiding a decode-back-to-`map` round
trip. Measured on a synthetic Function/CALLS graph, this cut peak heap for a
medium scale-lab slot (~190k nodes / 760k edges) from ~4.9 GiB to ~1.36 GiB with
byte-identical output (the `ifamemaudit`-tagged `TestMemAuditCanonicalizeScale`
and the `TestCanonicalizeGoldenDigests` byte-identity regression; see the
Performance Evidence marker below). The remaining levers for large/pathological
slots — streaming the output into the sha256 hash and an external merge-sort of
the record set — are deferred until those slots run routinely.

## Dependencies

`go/internal/replay` for the shared canonical JSON core
(`CanonicalizeValue`/`CanonicalOptions`) — see `doc.go`'s "Reused canonical
JSON core" section for why this package does not implement a second
canonicalizer. No other internal or external dependency.

## GCP scope integrity

`ValidateGCPProjectEdgeScopes` uses the same streaming `Reader` as
canonicalization to verify a caller-supplied, project-scoped fixture contract:
exact per-scope edge counts and `CloudResource` endpoint ownership. The caller
must supply every expected scope; unexpected GCP scopes fail closed. This does
not impose project-local ownership on valid organization, folder, or custom
collector scopes.

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
