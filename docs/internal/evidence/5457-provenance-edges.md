# #5457 graph provenance edges (PUBLISHES, BUILT_FROM): write-volume and cost evidence

Implements the `#5457` slice of the `#5472` graph-projection policy
(`docs/internal/design/5472-graph-projection-policy.md`): projects
`(:Repository)-[:PUBLISHES]->(:Package|:PackageVersion)` from package
ownership/publication correlation decisions, and
`(:ContainerImage)-[:BUILT_FROM]->(:Repository)` from container-image-identity
decisions, mirroring the `package_registry_edge_writer.go` Cypher shape and the
`kubernetes_correlation_edge_writer.go` sequential-autocommit retract dispatch.

## BUILT_FROM join-key decision: digest, not uid

`ContainerImageIdentityDecision`
(`go/internal/reducer/container_image_identity.go:63-82`) carries `ImageRef`
and `Digest` only — there is no field carrying the `OciImageManifest`/
`ContainerImage` node's `uid`. The canonical OCI writer
(`go/internal/storage/cypher/oci_registry_canonical_writer.go:35-45`) MERGEs
every `ContainerImage:OciImageManifest` node with both a `uid` property (its
MERGE identity) and a `digest` property (`m.digest = row.digest`, set
unconditionally). Since the decision struct has no uid to hand the writer, the
only available, non-fabricating join key is `digest`. `canonicalProvenanceBuiltFromCypher`
(`go/internal/storage/cypher/provenance_edge_writer.go`) therefore matches
`(img:ContainerImage {digest: row.digest})`, never `{uid: ...}`. A future
change that threads the manifest uid through `ContainerImageIdentityDecision`
could tighten this to a uid MATCH; until then, digest is correct and the only
option that does not require guessing or fabricating an identifier.

## Write-volume / cost-budget evidence

Tiering caps volume before any Cypher runs: `BUILT_FROM` admits only
`exact_digest` outcomes with a resolved `SourceRepositoryIDs` entry (stricter
than `PUBLISHES`, which also admits `derived`); `PUBLISHES` admits
`exact`/`derived` ownership and publication outcomes with a resolved
`RepositoryID`. All other outcomes (`ambiguous`/`unresolved`/`stale`/
`rejected`) produce zero rows before reaching the graph writer at all, per
`packageOwnershipPublishesRows`, `packagePublicationPublishesRows`, and
`containerImageBuiltFromRows`.

### B-9 (#3802) row-builder handler budgets

Benchmark Evidence: `go test ./internal/reducer -bench
'BenchmarkPackageOwnershipPublishesRows|BenchmarkPackagePublicationPublishesRows|BenchmarkContainerImageBuiltFromRows'
-benchtime=100ms -count=6`, darwin/arm64 (Apple M5 Max), 2026-07-23, 5000
decisions/rows per benchmark, median of 6 samples:

| Benchmark | Median ns/op | Budget (median×1.5) | B-9 gate |
| --- | --- | --- | --- |
| `BenchmarkPackageOwnershipPublishesRows` | 469,017 | 703,526 | PASS (measured 467,757 on the gate run) |
| `BenchmarkPackagePublicationPublishesRows` | 462,643 | 693,965 | PASS (measured 474,392 on the gate run) |
| `BenchmarkContainerImageBuiltFromRows` | 583,584 | 875,376 | PASS (measured 580,262 on the gate run) |

`scripts/verify-reducer-perf-gate.sh` output: "all 13 handlers within budget"
(the 10 pre-existing handlers plus these 3). Entries added to
`testdata/benchmarks/reducer-handler-budgets.txt`.

### Graph write batching

Both edge writers batch at `DefaultBatchSize` (500 rows/statement, via the
shared `buildBatchedStatements` helper) exactly like `package_registry_edge_writer.go`
and `kubernetes_correlation_edge_writer.go`. `PUBLISHES` rows are bucketed by
target label (Package vs PackageVersion) before batching, so a scope with N
ownership/publication decisions produces at most
`ceil(N_package/500) + ceil(N_version/500)` statements dispatched as one
atomic `ExecuteGroup` (or sequential `Execute` when the backend does not
support grouping). `BUILT_FROM` fans a decision's `SourceRepositoryIDs` out to
one row per distinct source repository, so worst-case row count is
`sum(len(SourceRepositoryIDs))` across exact-digest decisions, still batched
at 500.

### Retraction cost

Retraction is two (PUBLISHES: ownership + publication) or one (BUILT_FROM)
single-statement, sequential-autocommit `Execute` calls per reducer intent,
scoped by `scope_id + evidence_source` — O(1) statements regardless of prior
edge volume (the `DELETE` runs server-side over the anchored `MATCH`, not a
per-edge round trip). This mirrors
`kubernetes_correlation_edge_writer.go:250-267`'s dispatch cost.

## Proof matrix (positive/derived/negative/missing-endpoint/retraction/concurrency)

All cases proven with focused unit tests (fake `Executor`/writer doubles, no
live backend -- the orchestrator's Docker golden-corpus gate is the live-data
follow-up):

| Case | Test | Result |
| --- | --- | --- |
| Positive (exact -> edge) | `TestPackageOwnershipPublishesRowsAdmitsExactAndDerivedOnly`, `TestContainerImageBuiltFromRowsAdmitsExactDigestOnly` | green |
| Derived (ownership/publication) -> edge | same tests (derived case asserted alongside exact) | green |
| Negative (ambiguous/unresolved/stale/rejected/consumption) -> no edge | same tests (all five non-admitted outcomes asserted to produce zero rows); consumption correlation is never passed into the provenance row builders at all (`projectPackageProvenanceEdges` only takes ownership+publication decisions) | green |
| Missing endpoint -> no-op, never fabricate | `TestProvenanceEdgeWriterWritePublishesPackageMatchMatchMerge` / `TestProvenanceEdgeWriterWriteBuiltFromMatchesByDigest` assert two MATCHes precede every MERGE and no endpoint label is ever MERGEd | green |
| Retraction (retract-first per generation, idempotent re-run) | `TestProjectPackageProvenanceEdgesRetractsFirstThenWritesBothEvidenceSources`, `TestProjectPackageProvenanceEdgesRetractsEvenWhenNoRowsToWrite`, `TestProjectContainerImageBuiltFromEdgesRetractsFirstThenWrites`, `TestProjectContainerImageBuiltFromEdgesRetractsEvenWhenNoRowsToWrite` | green |
| Retract dispatch never uses ExecuteGroup | `TestProvenanceEdgeWriterRetractPublishesUsesSequentialExecuteNeverGroup`, `TestProvenanceEdgeWriterRetractBuiltFromUsesSequentialExecuteNeverGroup` | green |
| Concurrency/isolation (scope_id+evidence_source is the conflict domain) | `TestProvenanceEdgeWriterRetractPublishesUsesSequentialExecuteNeverGroup` asserts the retract predicate is scoped by both `scope_id` and `evidence_source`; ownership and publication use distinct evidence sources so their retracts never collide even within the same scope; BUILT_FROM's evidence_source (`reducer/container-image-identity`) never collides with the shared-edge-type `#5428` domain (`reducer/ci-cd-run-correlation`) | green (unit-level; no global lock introduced -- writes partition naturally by scope_id+evidence_source, never globally serialized) |

## Observability

Observability Evidence: `eshu_dp_provenance_edges_total` (registered
`go/internal/telemetry/instruments.go`), labeled by `domain` (the producing
evidence_source: `reducer/package-ownership`, `reducer/package-publication`,
`reducer/container-image-identity`) and `outcome` (`materialized`). X1 rows
added to `docs/public/observability/telemetry-coverage.md` for
`package_provenance_edges.go` and `container_image_provenance_edges.go`;
`scripts/verify-telemetry-coverage.sh` passes.

## Known follow-up (not completed in this change)

The B-12 golden-corpus snapshot (`testdata/golden/e2e-20repo-snapshot.json`)
`edge_counts`/`required_correlations` entries for `PUBLISHES`/`BUILT_FROM`
against the live `supply-chain-demo` 20-repo corpus are **not** added here.
Confirming a non-vacuous (>=1) live edge count requires the orchestrator's
Docker golden-corpus gate (explicitly out of scope for this focused-verify
pass) plus resolving a data-shape question found while investigating:
`testdata/cassettes/packageregistry/supply-chain-demo.json`'s
`package_registry.source_hint` fact for `github.com/acme/lib-common` carries
`version_id: "1.0.0"`, while the corresponding `package_registry.package_version`
fact's canonical `PackageVersion` uid is the composite
`github.com/acme/lib-common@1.0.0`. If that mismatch is real (not a
fixture-authoring artifact), the ownership decision for that hint will target
a non-existent `PackageVersion` uid and safely no-op (skipped, never a
fabricated or wrong edge) rather than produce the expected PUBLISHES edge --
worth resolving before adding a `minimum_count: 1` rc assertion so the gate
doesn't lock in a vacuous or flaky edge count. The replay-depth-requirements
delta_tombstone scenario for `BUILT_FROM`/`PUBLISHES` is also not yet backed
by a dedicated scenario file (tracked as an advisory gap in the regenerated
`docs/public/reference/replay-coverage.md`, does not fail the blocking breadth
gate).
