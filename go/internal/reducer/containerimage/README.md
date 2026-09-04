# containerimage

Resolves a container image reference to a canonical digest identity by
joining Git, OCI registry, and runtime evidence, then projects the exact
digest-addressed decisions into canonical BUILT_FROM and DERIVED_FROM graph
edges.

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns the `container_image_identity` reducer domain
and the pipeline behind it, and nothing else in the reducer depends on its
internals except through the root's compatibility aliases.

## What it owns

| piece | file | what it does |
|---|---|---|
| `ContainerImageIdentityHandler` | `container_image_identity.go` | the reducer handler for `container_image_identity` |
| `BuildContainerImageIdentityDecisions` | `container_image_identity.go` | joins Git/runtime/OCI/CI evidence into per-reference decisions |
| reference parsing | `container_image_identity_ref_parsing.go` | splits a raw reference into repository key + digest or tag |
| registry resolution | `container_image_identity_registry.go` | resolves a parsed reference against active OCI registry facts |
| SLSA/attestation join | `container_image_identity_slsa.go`, `container_image_identity_slsa_refs.go` | folds verified SLSA provenance into identity strength |
| retirement | `container_image_identity_retirement.go` | tombstones logical identities no longer supported by evidence |
| `PostgresContainerImageIdentityWriter` | `container_image_identity_writer.go` | the legacy outcome-keyed Postgres writer |
| `PostgresContainerImageIdentitySupportWriter` | `container_image_identity_support_writer.go` | the digest-v3 support-set Postgres writer |
| `ContainerImageProvenanceEdgeWriter` projection | `container_image_provenance_edges.go` | projects exact_digest decisions into BUILT_FROM edges |
| `ContainerImageDerivedFromEdgeWriter` projection | `container_image_derived_from_edges.go` | projects Dockerfile base-image lineage into DERIVED_FROM edges |
| `GraphContainerImageExistenceLookup` | `container_image_existence_lookup.go` | confirms a candidate target node actually exists before counting an edge |

## Exported surface

| symbol | what it is |
|---|---|
| `ContainerImageIdentityDecision` / `Write` / `WriteResult` | the decision, publication input, and result records |
| `ContainerImageIdentityHandler` | the reducer handler |
| `ContainerImageIdentityWriter` | the durable publication interface |
| `BuildContainerImageIdentityDecisions` / `...WithQuarantine` | the pure decision builders |
| `ParseContainerImageRef` / `ParsedContainerImageRef` / `NormalizeContainerRepositoryKey` / `DigestFromImageRef` | reference parsing, also used by root-staying Kubernetes correlation and AWS running-image resolution |
| `PostgresContainerImageIdentityWriter` / `PostgresContainerImageIdentitySupportWriter` | the two Postgres writer generations |
| `ContainerImageIdentityTransaction` / `ContainerImageIdentityBeginner` / `ContainerImageIdentityClaimedExecer` / `ContainerImageIdentityCutoverLookup` / `ContainerImageIdentityLegacyCleanupLookup` / `ContainerImageIdentityActivationEpochLookup` / `ContainerImageIdentityHeldSupportLoader` | the writer's narrow storage ports |
| `ContainerImageIdentityPriorSupport` | one prior authoritative support row eligible for carry-forward |
| `ContainerImageExistenceLookup` / `GraphContainerImageExistenceLookup` | the graph existence check the AWS materializer uses before counting an edge |
| `ContainerImageProvenanceEdgeWriter` / `ContainerImageDerivedFromEdgeWriter` | the BUILT_FROM / DERIVED_FROM graph write ports |
| `ContainerImageIdentityFormatImageRef` | the `identity_format` payload marker for the current image-ref encoding |
| `ErrContainerImageIdentityClaimRejected` | the atomic-write fencing error |
| `ContainerImageBuiltFromRows` / `ContainerImageDerivedFromRows` / `...ProvenanceEvidenceSource` / `ContainerImageIdentityPayload` | root-staying benchmark and unit tests still exercise this family's row-building and payload logic directly |
| `ContainerImageBuiltFromRowsForReplayTest` / `ContainerImageEffectiveRowsForReplayTest` / `ProjectEffectiveContainerImageIdentityEdgesForReplayTest` | replay-test-only exports for the root's cross-family cassette replay test |

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `reducer/cicdrun`,
`reducer/packagesourcecore`, `reducer/sbomattest`, `internal/facts`,
`internal/telemetry`, `internal/truth`, and the factschema SDK, and it never
imports the parent `internal/reducer` package. The dependency runs the other
way: the root keeps compatibility aliases in
`container_image_identity_compat.go` so its own callers, plus `cmd/reducer`,
`internal/storage/postgres`, and `internal/replay/costcounting`, compile
unchanged.

`GraphQueryRunner` and `activeRepositoryFactLoader` are declared locally
rather than imported from the root, which owns the canonical versions shared
by several still-in-root families. Go interfaces are structural, so the same
concrete implementations root wires in elsewhere satisfy these local
declarations too, without duplicating any logic — the established precedent
is `internal/reducer/codetaint/graph_ports.go`.

Two root-owned symbols this family used to reach as one-line forwarders
(`ociRepositoryID`, `boolPayload`) turned out to already forward straight to
`payloadcore.OCIRepositoryID` / `payloadcore.BoolPayload`, so this package
calls `payloadcore` directly for both rather than keeping a redundant second
hop, and the root's own remaining callers now forward to `payloadcore`
directly too.

## Telemetry

| stage | instrument | labels |
|---|---|---|
| identity decisions | `eshu_dp_container_image_identity_decisions_total` | `domain`, `outcome` |
| retirements | `eshu_dp_container_image_identity_retirements_total` | `domain`, `outcome` |
| BUILT_FROM / DERIVED_FROM projection | `eshu_dp_provenance_edges_total` | `domain`, `outcome` |

`eshu_dp_provenance_edges_total` is shared with every other producer of
canonical PUBLISHES, BUILT_FROM, and DERIVED_FROM rows (package-source
correlation, CI/CD workflow-image edges); an operator filters by the `domain`
attribute rather than expecting a family-specific instrument. Facts rejected
for a malformed payload feed the shared
`eshu_dp_reducer_input_invalid_facts_total` counter through `factdecode`
instead of a family-specific one, and the reducer executions that run this
handler stay covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk inside the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders or aliases (`Intent`, `Result`,
`ContainerImageIdentityOutcome` and its constants, `FactLoader`,
`quarantinedFact`, the `reducerFact*` batch-insert family, `derefString` and
the other `payloadStr`-family helpers, the `decodeAWS/Azure/GCP/OCI/CICD*`
schema decoders, `cicdRunKeyFromParts`/`trimmedCICDPtr`, and the
`packageSource*` matching helpers) are now imported from the leaf package
that already owned them. `ParseContainerImageRef` /
`NormalizeContainerRepositoryKey` / `DigestFromImageRef` /
`ContainerImageIdentityFormatImageRef` and the four fields of
`ParsedContainerImageRef` were exported (from unexported originals) because
root-staying Kubernetes correlation, AWS running-image resolution, and
supply-chain-impact anchor consensus name them directly; each keeps a root
forwarder under its former unexported spelling so no root call site changed.
`GraphQueryRunner` and `activeRepositoryFactLoader` are locally redeclared,
not imported, for the reason above. A Go import change and an unexported
struct field becoming exported add no indirection or behavior change at
runtime. Measured on this branch after the final edit: `go build ./...` and
`go vet ./...` both exit 0, and `go test ./internal/reducer/... -count=1`
passes, including this package.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
or log field. The three counters above and the reducer executions that wrap
them are the same before and after the move; only the file paths the
telemetry-coverage rows point at changed.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, hoist it to a shared-core tier
  (`payloadcore`/`contract`/etc.) with a root forwarder, or — if the symbol
  is genuinely root-owned and shared by other still-in-root families —
  redeclare a structurally identical interface locally, the way
  `GraphQueryRunner` and `activeRepositoryFactLoader` do here.
- **A `_test.go` file's exports never cross a package boundary.** The root's
  pre-#6061 pattern (`provenance_replay_export_test.go` exposing
  package-private helpers to `reducer_test`) only works within one
  directory's own test binary. Cross-package test needs — the root's
  cross-family replay and provenance-edge-counter tests — are served by
  ordinary exported functions/methods in non-`_test.go` files instead
  (`container_image_identity_replay_export.go`,
  `container_image_identity_root_compat_exports.go`), never called by
  production code.
- **`container_image_identity_cicdrun_cassette_test.go` is `package
  containerimage_test` (external), not `package containerimage`.** It
  imports `internal/replay/cassette`, which transitively imports the reducer
  root, which imports this package back for its compatibility aliases. An
  internal test file would fold that whole chain into this package's own
  test build and create an import cycle that `go build` does not see but
  `go vet`/`go test` does.
- **`ParsedContainerImageRef.RepositoryKey` must match the OCI registry
  collector's own normalization**, or a digest observation never joins its
  Git/CI-sourced reference.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
