# Tag/Digest Mutation-History Read Surface Evidence

Issue #5459 exposes already-captured `ContainerImageTagObservation` history
through a bounded, ordered query (`GET /api/v0/images/tag-history`) and MCP
tool (`list_container_image_tag_history`), and adds `first_observed_at` --
the FIRST queryable node-property timestamp in the canonical graph.

## The #1 constraint: set-once, never overwritten

`first_observed_at` is written with `ON CREATE SET t.first_observed_at =
row.observed_at` inside the tag-observation identity MERGE
(`canonicalOCIImageTagObservationUpsertCypher`), so it is fixed once at node
creation and never overwritten by a later observation of the same uid. `ON
CREATE` fires only when the node is created, so it reads no persisted property
and needs no separate statement or deferred phase.

Three prior shapes were disproven live on NornicDB before this one, driven by
the mandatory prove-theory-first gate — the reason the arrival at `ON CREATE
SET` cost zero production rework:

1. A compound self-referencing guard (`CASE`) or `coalesce` inside the same
   `UNWIND ... MERGE ... SET` regressed to last-write-wins: the MERGE binding
   shadows the persisted property as null for any same-statement self-read.
2. A separate DEFERRED second-transaction
   `MATCH (t {uid}) WHERE t.first_observed_at IS NULL SET ...` passed the
   isolated shim but FAILED the live golden-corpus pipeline — the deferred
   MATCH did not surface the multi-label
   `(:ContainerImageTagObservation:OciImageTagObservation)` node the identity
   MERGE created across the write group's transaction boundary, so
   `first_observed_at` was never populated (`corpus-gate`:
   `result item missing required field "first_observed_at"`).
3. `ON CREATE SET` in the single identity MERGE holds the first value with no
   self-reference and no cross-transaction dependency.

**Cite:** `go/internal/storage/cypher/oci_tag_first_observed_prove_theory_live_test.go`
(`TestLiveOCITagFirstObservedProveTheory`, skipped by default, gated on
`ESHU_OCI_TAG_PROVE_LIVE=1` against a live NornicDB) proves live that `ON
CREATE SET` holds the FIRST-created value (`t2`) through later out-of-order
replay of an earlier (`t1`) and a later (`t3`) value, and that eight concurrent
writers to the same uid converge to exactly one valid RFC3339 observation with
no corruption or null loss.

## Performance Evidence

The read is a single indexed `MATCH (t:ContainerImageTagObservation
{image_ref: $image_ref})` anchored on the existing
`container_image_tag_observation_ref` index over `image_ref` -- an indexed
equality lookup, not a label scan -- bounded by `limit+1` with deterministic
`ORDER BY t.first_observed_at, t.uid` and offset continuation, mirroring
`ImageHandler`'s existing `/api/v0/images` shape exactly. The write side adds
only an `ON CREATE SET` clause and one `observed_at` property to the existing
tag-observation identity MERGE; it adds no new statement, no new index, no new
scan, and no new lock beyond the existing per-uid MERGE the identity upsert
already performs.

## No-Regression Evidence

`go test ./internal/storage/cypher -run
'OCITagFirstObserved|OCITagObserv|CanonicalNodeWriter' -count=1` proves: the
identity upsert writes `first_observed_at` via `ON CREATE SET` and that
`first_observed_at` appears exactly once (never in the unconditional `SET`, so
it cannot be overwritten on a later match); the identity row carries the
RFC3339-UTC `observed_at` value the `ON CREATE SET` consumes; and a zero-value
`ObservedAt` serializes to `""` (so the node is created without a meaningful
`first_observed_at` and the reader omits it) rather than a Unix-epoch string. `go test ./internal/query -run 'TagHistory' -count=1` (9 tests)
proves the handler's happy path ordering, default/explicit/out-of-range limit
validation, truncation plus `next_cursor`, the required
`repository_id`+`tag` selector (missing or malformed selector rejected with
400, never an empty 200), nil-backend 503, and unsupported-capability 501.
`go test ./internal/mcp -run 'TagHistory|GoldenSnapshot|Tools' -count=1` (87
tests) proves the MCP tool is registered, its schema advertises the required
selector, `resolveRoute` composes the bounded query, and an end-to-end
`dispatchTool` call through a real `query.TagHistoryHandler` returns the
ordered `tag_history` rows under the canonical truth envelope.

This change adds an `oci_registry.image_tag_observation` cassette fact
(`testdata/cassettes/ociregistry/supply-chain-demo.json`) and a
`query_shapes.mcp["list_container_image_tag_history"]` assertion
(`minimum_results: 1`, `first_observed_at` required) to the B-12 snapshot, so
the golden-corpus gate is affected. `go test ./cmd/golden-corpus-gate
./internal/goldengate -count=1` and `go test ./internal/graph -count=1`
validate the snapshot+cassette contract structurally (schema, per-tool
coverage, snapshot parse, tool-count lockstep).

The full live end-to-end assertion — that the cassette fact projects a
`ContainerImageTagObservation` node carrying `first_observed_at` and the MCP
tool returns it (`minimum_results >= 1`) — was run locally to green:

```
$ NEO4J_BOLT_PORT=7787 ESHU_POSTGRES_PORT=15532 ... \
    bash scripts/verify-golden-corpus-gate.sh
  [PASS] mcp:list_container_image_tag_history: "tag_history" has 1 results;
         item fields [tag resolved_digest first_observed_at] present
  === PASS: B-7 golden corpus gate green (elapsed 34s) ===
```

(The compose host ports are `${NEO4J_BOLT_PORT:-7687}` / `${ESHU_POSTGRES_PORT:-15432}`
env-overrides, so the gate runs on remapped ports in its own compose project
without disturbing a concurrent base stack.) This run is what caught the
original defect: an earlier DEFERRED-MATCH set-once shape passed the isolated
prove-theory shim but left `first_observed_at` unpopulated in the real
pipeline (`corpus-gate: result item missing required field "first_observed_at"`),
because the deferred multi-label MATCH did not surface the node across the
write group's transaction boundary. The `ON CREATE SET` shape fixed it, and the
gate above confirms the field is present end-to-end.

## Observability Evidence

`TagHistoryHandler` carries a dedicated handler span
(`telemetry.SpanQueryContainerImageTagHistory`,
`query.container_image_tag_history`) plus a package-local duration histogram
(`eshu_dp_query_container_image_tag_history_duration_seconds`) and error
counter (`eshu_dp_query_container_image_tag_history_errors_total`) with a
bounded `outcome`/`reason` label, mirroring `ImageHandler`'s existing
`images_telemetry.go` pattern exactly (`imageListDuration`/`imageListErrors`).
Registration failures leave the instruments nil so a telemetry pipeline fault
never fails the read. The write side adds only an `ON CREATE SET` clause to the
existing `oci_registry` phase MERGE, so it reuses the existing
`CanonicalNodeWriter` phase-span/duration logging (`logCanonicalPhaseFailure`,
`recordAtomicWrite`) with no new span, metric, or write phase.

## Known limitations (by design, not gaps)

1. Identity is keyed by `(repository_id, tag, resolved_digest)`, so a tag that
   flips back to a previously observed digest (A -> B -> A) collapses onto the
   SAME node it originally created. The returned "order digests changed"
   history is bounded by the distinct-digest set observed for the tag, not a
   full chronological event log of every transition.
2. `first_observed_at` is a set-once value: it holds the FIRST projected
   observation and never regresses under a later or out-of-order
   re-projection. A back-dated observation arriving after a later one is not
   reflected. `last_observed_at` and true per-event history are deferred to
   follow-up work.
