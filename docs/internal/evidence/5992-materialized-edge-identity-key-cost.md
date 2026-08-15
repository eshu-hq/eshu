# #5992 — materialized-edge identity key cost

## What changed

`cypher.MaterializedEdgeIdentityProperties` declares, per materialized-edge
family, the relationship properties beyond a type's two endpoint nodes that
participate in its MERGE identity. Twelve of the fourteen families MERGE on
endpoints alone and declare an empty set; `codeowners_ownership_edges`
(`DECLARES_CODEOWNER`: `pattern`, `source_path`) and `submodule_pin_edges`
(`PINS_SUBMODULE`: `path`) fold a property into their MERGE key because two
distinct source rows can otherwise collide onto the same (source, target)
relationship pattern.

`ifa.ExpectedEdge` gained an `Identity map[string]string` field, and `Key()`
appends it in sorted property-name order when non-empty. `LoadExpectedEdges`
validates a fixture's identity against the family's declaration, and
`cmd/ifa/assert_edges.go`'s `assertMaterializedEdges` mirrors the same
projection against the LIVE graph, reading declared properties off
`edge.Props` and folding them into the same `Key()` the fixture side builds —
once per streamed graph edge, inside a live-gate assertion, not a request or
write path.

## No-Regression Evidence

No-Regression Evidence: `Key()` with a nil or empty `Identity` takes the
byte-identical early-return path that existed before this field, so every
family with no declared identity (twelve of fourteen, including the two
already-proven families `sql_relationships` and `code_calls`) pays no added
cost and needs no re-proof — pinned by
`TestExpectedEdgeKeyIsByteIdenticalWithNoIdentity` and
`TestLoadExpectedEdgesCommittedFixturesStillLoad`
(`go/internal/ifa/materialized_edges_assert_test.go`).

The added cost applies ONLY to the two families that declare identity
properties. `BenchmarkExpectedEdgeKey` measures both paths directly (same
file):

```
go test ./internal/ifa/... -run '^$' -bench BenchmarkExpectedEdgeKey -benchmem -count=3
```

Apple M5 Max, three iterations each:

```
BenchmarkExpectedEdgeKey/nil_identity_(before)-18           21.6–22.8 ns/op    48 B/op   1 allocs/op
BenchmarkExpectedEdgeKey/two-property_identity_(after)-18  103.8–113.7 ns/op   192 B/op  3 allocs/op
```

Independently reproduced on a second machine (same benchmark, same shapes):

```
BenchmarkExpectedEdgeKey/nil_identity_(before)-18          23.99 ns/op    48 B/op   1 allocs/op
BenchmarkExpectedEdgeKey/two-property_identity_(after)-18  112.6  ns/op  192 B/op   3 allocs/op
```

Both runs land in the same range: roughly a 5x per-key cost for the two
identity-bearing families, still sub-microsecond.

### Update: the injectivity fix raised the identity-path cost

Review found `Key()`'s original identity-suffix encoding (raw `"|"`-joined
components) was not injective -- two structurally distinct edges could render
the same string (see `TestExpectedEdgeKeyIsInjective`). The fix switches the
non-empty-`Identity` path to a length-prefixed ("<byte length>:<content>")
encoding of every component, which costs more per key: an initial
`fmt.Fprintf`-based implementation measured 440-461 ns/op, 312 B/op, 11
allocs/op -- roughly 4x the pre-fix "after" number. Replacing `fmt.Fprintf`
with direct `strconv.Itoa` plus `strings.Builder` writes
(`writeLengthPrefixedField`) brought it back down:

```
BenchmarkExpectedEdgeKey/nil_identity_(before)-18            26.5–26.7 ns/op    48 B/op   1 allocs/op
BenchmarkExpectedEdgeKey/two-property_identity_(after)-18   161.4–167.7 ns/op  176 B/op   4 allocs/op
```

The nil-identity path is unchanged (it never runs the length-prefixed
encoding at all -- confirmed by `TestExpectedEdgeKeyIsByteIdenticalWithNoIdentity`
still passing byte-for-byte). The identity-bearing path now costs about 6x
the nil path instead of 5x, still under 200 ns per key. At the same
10^4-10^5 edges-per-assertion scale, that is roughly 1.6 ms to 16.4 ms of
added CPU time for a family entirely composed of identity-bearing edges in
the worst case -- still negligible against a live gate whose wall time is
dominated by minutes of Docker bring-up and Bolt streaming. The other twelve
families see no change: their edges stay on the nil-identity path at the
"before" cost with a byte-identical key.

A precomputed key (caching `Key()` on `ExpectedEdge` or the live edge struct)
was considered and rejected: it would add a cache-invalidation surface —
keeping a cached key consistent with a mutated `Identity` map across the
fixture-load and live-stream paths — for a cost that this measurement shows
is irrelevant at the scale these gates run at.

## Observability Evidence

No-Observability-Change: no new metric, span, or log is added. The
`identityErrs` bucket this precursor adds to `assertMaterializedEdges`
surfaces through the existing `assert-edges` failure report (exit code plus
stderr text, alongside the existing `missing`/`extra`/`duplicate`/
`endpointErrs` sections) — the same reporting surface every other exact-set
defect in this command already uses. Nothing here changes what an operator's
dashboards or alerts see.
