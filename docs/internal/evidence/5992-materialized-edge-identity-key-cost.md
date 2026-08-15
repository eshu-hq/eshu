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

### Revision: every comparison key is now length-prefixed, not just the identity suffix

No-Regression Evidence: the section below this one is the ORIGINAL evidence, from when only `Key()`'s
`Identity`-bearing suffix used a length-prefixed encoding and the
no-`Identity` path stayed byte-identical to the pre-`Identity` raw `"|"`-join.
Code review (two rounds) found that split left real injectivity defects
behind: `Key()`'s no-`Identity` path delegates to `sqlRelationshipEdgeKey`,
which still joined `RelationshipType`, `SourceEntityID`, and `TargetEntityID`
with a raw, unescaped `"|"` — the same defect class, just one level down —
and `codeCallEdgeKey` (`code_calls`'s own comparison key, unrelated to
`Key()`) had it too. `code_calls` is already proven live on both live gates;
a `"|"` inside a code-entity uid (legal — these uids are path-derived) could
silently collapse two of its five expected edges, with the exact-set
assertion unable to tell (`TestCodeCallEdgeKeyIsInjective`,
`TestSQLRelationshipEdgeKeyIsInjective`, both RED before the fix and GREEN
after).

Verified before changing the encoding: every consumer of `Key()`,
`sqlRelationshipEdgeKey`, and `codeCallEdgeKey` builds the key fresh, in
memory, on both sides of a `map` or `==` comparison inside one process run —
grepped across `internal/ifa` and `cmd/ifa`. Nothing persists a key, logs it
as a stored artifact, or compares it against a fixture-authored literal, so
changing the byte format cannot invalidate a prior live-gate proof: what
those proofs established is that the same extraction logic reproduces the
same edge set, which holds regardless of how set membership is encoded. All
three functions now share ONE injective, length-prefixed
(`writeLengthPrefixedField`) encoding, so `Key()` no longer branches on
encoding at all — only on whether `Identity` fields exist to append.

The practical consequence: the twelve families that declare no identity no
longer get a free, near-zero-cost key — `sqlRelationshipEdgeKey` (their
shared delegate) now pays the same length-prefixing cost the two
identity-bearing families always did, just for three fields instead of five
or seven. Current numbers, Apple M5 Max,
`go test ./internal/ifa/... -run '^$' -bench BenchmarkExpectedEdgeKey -benchmem -count=3`:

```
BenchmarkExpectedEdgeKey/nil_identity_(before)-18            67.9–70.9 ns/op  120 B/op  4 allocs/op
BenchmarkExpectedEdgeKey/two-property_identity_(after)-18   140.2–141.5 ns/op 176 B/op  4 allocs/op
```

At 10^4-10^5 edges per `assert-edges` invocation, that is roughly 0.7 ms to
7.1 ms of CPU time for every family (up from effectively zero before), and
roughly 1.4 ms to 14.1 ms for a family entirely composed of identity-bearing
edges in the worst case — still negligible against a live gate whose wall
time is dominated by minutes of Docker bring-up and Bolt streaming, and the
`nil` path's own before/after numbers below show this class of cost was
never actually free; it was only unmeasured until an injectivity fix forced
the comparison.

A precomputed key (caching `Key()` on `ExpectedEdge` or the live edge
struct) was considered and rejected, now for a stronger reason than before:
correctness, not just cost, is at stake, and caching would add a
cache-invalidation surface across the fixture-load and live-stream paths for
a saving these numbers show is not needed at this scale.

### Original evidence: identity-suffix-only length-prefixing (superseded above)

`Key()` with a nil or empty `Identity` took the byte-identical early-return
path that existed before the `Identity` field, so every family with no
declared identity (twelve of fourteen, including the two already-proven
families `sql_relationships` and `code_calls`) paid no added cost and needed
no re-proof — pinned at the time by `TestExpectedEdgeKeyIsByteIdenticalWithNoIdentity`
(since renamed and repurposed;
see `TestExpectedEdgeKeyEmptyIdentityMatchesSQLRelationshipEdgeKey`) and
`TestLoadExpectedEdgesCommittedFixturesStillLoad`.

`BenchmarkExpectedEdgeKey` measured both paths directly at that point:

```
BenchmarkExpectedEdgeKey/nil_identity_(before)-18           21.6–22.8 ns/op    48 B/op   1 allocs/op
BenchmarkExpectedEdgeKey/two-property_identity_(after)-18  103.8–113.7 ns/op   192 B/op  3 allocs/op
```

Independently reproduced on a second machine (same benchmark, same shapes):

```
BenchmarkExpectedEdgeKey/nil_identity_(before)-18          23.99 ns/op    48 B/op   1 allocs/op
BenchmarkExpectedEdgeKey/two-property_identity_(after)-18  112.6  ns/op  192 B/op   3 allocs/op
```

An intermediate `fmt.Fprintf`-based implementation of the length-prefixed
encoding cost 440-461 ns/op, 312 B/op, 11 allocs/op — roughly 4x the
`fmt`-free version — before being replaced with direct `strconv.Itoa` plus
`strings.Builder` writes (`writeLengthPrefixedField`), which brought it down
to 161.4-167.7 ns/op, 176 B/op, 4 allocs/op at that time. See the revision
above for the current, final numbers now that the same encoding covers every
path.

## Observability Evidence

No-Observability-Change: no new metric, span, or log is added. The
`identityErrs` bucket this precursor adds to `assertMaterializedEdges`
surfaces through the existing `assert-edges` failure report (exit code plus
stderr text, alongside the existing `missing`/`extra`/`duplicate`/
`endpointErrs` sections) — the same reporting surface every other exact-set
defect in this command already uses. Nothing here changes what an operator's
dashboards or alerts see.
