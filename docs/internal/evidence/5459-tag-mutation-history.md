# Tag/Digest Mutation-History Read Surface Evidence

Issue #5459 exposes already-captured `ContainerImageTagObservation` history
through a bounded, ordered query (`GET /api/v0/images/tag-history`) and MCP
tool (`list_container_image_tag_history`), and adds `first_observed_at` --
the FIRST queryable node-property timestamp in the canonical graph.

## The #1 constraint: set-once, never fused

`first_observed_at` is written by a SEPARATE, DEFERRED second-transaction
statement (`canonicalOCIImageTagFirstObservedSetOnceCypher`,
`UNWIND $rows AS row MATCH (t:ContainerImageTagObservation {uid: row.uid})
WHERE t.first_observed_at IS NULL SET t.first_observed_at = row.observed_at`),
never fused into the existing tag-observation identity
MERGE/SET (`canonicalOCIImageTagObservationUpsertCypher`). The
`oci_tag_first_observed` phase runs in the same deferred second `ExecuteGroup`
as the `package_registry_*_edges` phases
(`isDeferredPackageRegistryEdgePhase`, `partitionDeferredPackageRegistryEdgePhases`
in `go/internal/storage/cypher/canonical_node_writer.go`), because the
`(:ContainerImageTagObservation:OciImageTagObservation)` node is multi-label
and NornicDB does not surface a multi-label node MERGE'd earlier in the same
transaction to a later same-transaction UNWIND-driven MATCH.

**Cite:** `go/internal/storage/cypher/oci_tag_first_observed_prove_theory_live_test.go`
(`TestLiveOCITagFirstObservedProveTheory`, skipped by default, gated on
`ESHU_OCI_TAG_PROVE_LIVE=1` against a live NornicDB) proves live that:

1. A compound self-referencing guard or `coalesce` inside the same
   `UNWIND ... MERGE ... SET` statement regresses to last-write-wins (the
   MERGE binding shadows the persisted property as null for any same-statement
   self-read).
2. The two-statement set-once shape holds the FIRST-written value (`t2`)
   through later out-of-order replay of an earlier (`t1`) and a later (`t3`)
   value.
3. Eight concurrent writers to the same uid converge to exactly one valid
   RFC3339 observation with no corruption or null loss.

## Performance Evidence

The read is a single indexed `MATCH (t:ContainerImageTagObservation
{image_ref: $image_ref})` anchored on the existing
`container_image_tag_observation_ref` index over `image_ref` -- an indexed
equality lookup, not a label scan -- bounded by `limit+1` with deterministic
`ORDER BY t.first_observed_at, t.uid` and offset continuation, mirroring
`ImageHandler`'s existing `/api/v0/images` shape exactly. The write side adds
exactly one deferred `UNWIND ... MATCH ... WHERE ... IS NULL ... SET`
statement per generation to the existing `oci_registry` write path; it adds no
new index, no new scan, and no new lock beyond the existing per-uid MERGE the
identity upsert already performs.

## No-Regression Evidence

`go test ./internal/storage/cypher -run
'OCITagFirstObserved|CanonicalNodeWriter|Partition' -count=1` (98 tests)
proves: the identity upsert statement never carries `first_observed_at`; the
set-once statement is separate, carries the RFC3339-UTC `observed_at` param,
and never MERGEs; the new `oci_tag_first_observed` phase partitions into the
deferred second-transaction group
(`partitionDeferredPackageRegistryEdgePhases`); and a zero-value `ObservedAt`
observation is omitted from the set-once batch rather than writing a zero
timestamp. `go test ./internal/query -run 'TagHistory' -count=1` (9 tests)
proves the handler's happy path ordering, default/explicit/out-of-range limit
validation, truncation plus `next_cursor`, the required
`repository_id`+`tag` selector (missing or malformed selector rejected with
400, never an empty 200), nil-backend 503, and unsupported-capability 501.
`go test ./internal/mcp -run 'TagHistory|GoldenSnapshot|Tools' -count=1` (87
tests) proves the MCP tool is registered, its schema advertises the required
selector, `resolveRoute` composes the bounded query, and an end-to-end
`dispatchTool` call through a real `query.TagHistoryHandler` returns the
ordered `tag_history` rows under the canonical truth envelope. `go test
./cmd/golden-corpus-gate ./internal/goldengate -count=1` and `go test
./internal/graph -count=1` are unaffected (pass unchanged).

## Observability Evidence

`TagHistoryHandler` carries a dedicated handler span
(`telemetry.SpanQueryContainerImageTagHistory`,
`query.container_image_tag_history`) plus a package-local duration histogram
(`eshu_dp_query_container_image_tag_history_duration_seconds`) and error
counter (`eshu_dp_query_container_image_tag_history_errors_total`) with a
bounded `outcome`/`reason` label, mirroring `ImageHandler`'s existing
`images_telemetry.go` pattern exactly (`imageListDuration`/`imageListErrors`).
Registration failures leave the instruments nil so a telemetry pipeline fault
never fails the read. The write side reuses the existing
`CanonicalNodeWriter` phase-span/duration logging
(`logCanonicalPhaseFailure`, `recordAtomicWrite`) with no new span or metric
name; the new `oci_tag_first_observed` phase is tagged with its own
`StatementMetadataPhaseKey` value (not `oci_registry`) so operator phase-level
diagnostics (`logProfiledStatement`'s `write_phase` attribute) correctly
distinguish it from the main OCI registry identity upserts.

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
