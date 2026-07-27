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

## B-12 golden-corpus snapshot (resolved)

The `version_id` mismatch flagged above was real and has been fixed
(`testdata/cassettes/packageregistry/supply-chain-demo.json`, coordinator
commit `fb5e2d73c5`): the `package_registry.source_hint` fact for
`github.com/acme/lib-common` now carries the canonical composite
`version_id` (`github.com/acme/lib-common@1.0.0`, matching the real
`source_hint.go:44` collector's `package_id + "@" + version` shape and the
`package_registry.package_version` fact's own `version_id`, the
`PackageVersion` node's uid) instead of the raw `"1.0.0"`.

`testdata/golden/e2e-20repo-snapshot.json` now carries:

- `edge_counts.PUBLISHES`: `{min: 1, max: 20}`. `github.com/acme/lib-common`'s
  single `source_hint` resolves (its `normalized_url`,
  `https://github.com/acme/lib-common`, matches the in-corpus `lib-common`
  fixture repo's `ESHU_GITHUB_ORG=acme`-synthesized remote) to one exact/derived
  decision via BOTH the ownership path and the publication join-by-version
  path, converging on **one** `Repository->PackageVersion` edge -- Cypher
  `MERGE` identity is the pattern only, so both writes upsert the SAME edge;
  properties come from whichever call ran last. `projectPackageProvenanceEdges`
  always writes ownership rows before publication rows within one `Handle()`
  call, so the edge's `evidence_kinds`/`evidence_source` deterministically end
  up publication's. (This is the min/max only; the `PUBLISHES`
  `Repository->Package` target is never hit by this cassette, since the one
  hint always carries a version-scoped id, per `packageOwnershipPublishesRows`'
  version-over-package precedence -- that target still no-ops safely, per the
  proof matrix above, if a future package-level-only hint is added.)
- `edge_counts.BUILT_FROM`: `{min: 1, max: 10}`. The `cicdrun` cassette's
  `ci.run` `artifact_digest` exactly matches the `ociregistry` cassette's
  `OciImageManifest` digest and carries `repository_id: repository:r_69256c06`,
  so `container_image_identity` resolves one `exact_digest` decision with that
  repository in `SourceRepositoryIDs`.
- `required_correlations` rc-164 (`PUBLISHES`, `Repository`->`PackageVersion`,
  `minimum_count: 1`, no `evidence_kinds`) and rc-165 (`BUILT_FROM`,
  `ContainerImage`->`Repository`, `minimum_count: 1`,
  `evidence_kinds: [CONTAINER_IMAGE_IDENTITY_EXACT_DIGEST]`,
  `required_edge_properties: [source_tool]`,
  `allowed_edge_property_values.source_tool: [oci]`).

**Why rc-164 has no `evidence_kinds` narrowing but rc-165 does.** The golden
gate's shared-verb isolation (`RequiredCorrelation.EvidenceKinds`,
`CountCorrelationWithEvidence` in `go/cmd/golden-corpus-gate/graph.go`) filters
by the edge's `evidence_kinds` list property in application code, not Cypher --
a NornicDB `WHERE` clause over an arbitrary relationship property does not
filter (see that function's own comment). `PUBLISHES` is written by no other
reducer domain, so it needs no narrowing (Tier-1 self-labeling by edge type,
the same class as rc-24 `HAS_VERSION`/rc-9 `DEPENDS_ON_PACKAGE`, per
`docs/public/reference/edge-source-tool-provenance.md`). `BUILT_FROM` IS shared
with the #5428 `reducer/ci-cd-run-correlation` domain, so rc-165 must narrow --
and `go/cmd/golden-corpus-gate/snapshot_test.go`'s
`TestEvidenceNarrowedCorrelationsRequireSourceTool` additionally requires any
`evidence_kinds`-narrowed rc to also pin `source_tool`. `provenance_edge_writer.go`
now stamps `rel.evidence_kinds` on every edge (all three evidence sources) and
`rel.source_tool = "oci"` specifically for `container-image-identity`
(added to the canonical vocabulary,
`docs/public/reference/edge-source-tool-provenance.md`); `PUBLISHES` rows are
never given a `source_tool` (no ecosystem-detection wired to the decision, so
an absent value beats a guessed one).

Verification: `go test ./cmd/golden-corpus-gate ./internal/goldengate -count=1`
passes (snapshot parses, `TestEvidenceNarrowedCorrelationsRequireSourceTool`
and every other snapshot-shape test green). Live confirmation that these
counts actually materialize against the Docker `supply-chain-demo` corpus is
the orchestrator's Docker golden-corpus gate run, per the task boundary this
executor was given.

## Truth-contract decision (item 7): no separate materialization domain needed

`PUBLISHES`/`BUILT_FROM` write inline from `PackageSourceCorrelationHandler.Handle`
and `ContainerImageIdentityHandler.Handle` -- there is no separate queued
reducer intent/domain for the graph projection (unlike, say,
`kubernetes_correlation` vs `kubernetes_correlation_materialization`, which are
two distinct `Domain`s with two distinct `Handle()` entry points). The existing
`packageSourceCorrelationDomainDefinition()` (`CanonicalKind:
"package_source_correlation"`) and `containerImageIdentityDomainDefinition()`
(`CanonicalKind: "container_image_identity"`) already declare
`OwnershipShape.CanonicalWrite: true`, and since the new edges are written
inside those SAME domains' `Handle()` calls, they are correctly attributed to
those existing truth contracts.

Confirmed by running the full `internal/truth` suite and every reducer
domain/contract/registry test with zero changes needed:

```
go test ./internal/truth/... -count=1
ok  	github.com/eshu-hq/eshu/go/internal/truth	0.462s

go test ./internal/reducer -run 'Contract|Domain|Default' -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	0.953s
```

Neither run required or benefited from a new `Domain`/`DomainDefinition`. In
particular, `TestMaterializedEdgeFamiliesLocksToAllProjectionDomains` (the one
test that enumerates reducer-owned graph-projecting domains) passed unchanged:
its `allProjectionDomains` list is scoped to the shared-projection-intent lane
(`repo_dependency`, `code_calls`, etc. -- 12 domains, per
`materialized_edge_families.go`'s own doc comment), which `package_source_correlation`
and `container_image_identity` were never part of even before this change
(they write edges directly, not through that shared lane). No gate anywhere
checks for a per-projecting-domain sibling `truth.Contract`; the additive
sibling pattern (`kubernetes_correlation_materialization` et al.) exists
because those domains have a genuinely separate queued intent/readiness-gate
lifecycle, which `PUBLISHES`/`BUILT_FROM` do not.

## Remaining known gap (advisory only)

The replay-depth-requirements delta_tombstone scenario for `BUILT_FROM`/`PUBLISHES`
is not backed by a dedicated scenario file (tracked as an advisory gap in the
regenerated `docs/public/reference/replay-coverage.md`, and as a follow-up in
[#5712](https://github.com/eshu-hq/eshu/issues/5712); confirmed non-blocking
by `bash scripts/verify-replay-coverage-gate.sh`'s "PASS ... (advisory)" exit,
and by `specs/replay-depth-requirements.v1.yaml`'s own header comment that
depth requirements "never fail the blocking breadth gate").
