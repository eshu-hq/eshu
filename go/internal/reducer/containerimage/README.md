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
| `ContainerImageIdentitySupportPublishExecer` / `ContainerImageIdentitySupportPublishResult` | the digest-v3 support publish port, which additionally reports the previous-`last_set_id` comparison (#8284) |
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
`reducer/packages/source`, `reducer/sbomattest`, `internal/facts`,
`internal/telemetry`, `internal/truth`, and the factschema SDK, and it never
imports the parent `internal/reducer` package. The dependency runs the other
way: the root keeps compatibility aliases in
the container-image stanza of `compat_cloud.go` so its own callers, plus `cmd/reducer`,
`internal/storage/postgres`, and `internal/replay/costcounting`, compile
unchanged.

`GraphQueryRunner` and `activeRepositoryFactLoader` are declared locally
rather than imported from the root, which owns the canonical versions shared
by several still-in-root families. Go interfaces are structural, so the same
concrete implementations root wires in elsewhere satisfy these local
declarations too, without duplicating any logic — the established precedent
is `internal/reducer/code/taint/graph_ports.go`.

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

## Unchanged-replay completion suppression (#8284)

Every succeeded identity ACK used to emit a coalesced
`cross_scope_completion_events` row even when its support set was
byte-identical, and the fanout reopened succeeded `supply_chain_impact` (and
`ci_cd_run_correlation`) consumers for each one. Measured over the 5.8h
window after the 2026-10-19 rollout restart: 14,717 succeeded identity
executions drove 858 `supply_chain_impact` replays (~74/hr per work item)
costing 13,665 handler-seconds at a 15.9s mean — the largest handler-wall
total of any reducer domain — while the other producer
(`ci_cd_run_correlation`) ran zero times, all 152 scopes held exactly one
support set each, and the downstream output stayed empty (0 findings, 0
canonical winners across all 858 replays).

The support publish now compares the resolved set against the pre-update
`last_set_id` under the scope-state row lock and reports the verdict through
`SupportChangedSignal`; both identity ACK paths emit only for changed (or
unwired, fail-open) runs. The comparison deliberately uses `last_set_id`,
which activation preserves, not `active_set_id`, which activation resets to
NULL — so a byte-identical first publish after an activation stays
suppressed while a changed one still reopens consumers. No worker or batch
shape changed: every item still ACKs to succeeded, only the event INSERT is
filtered.

No-Regression Evidence: `go test ./internal/reducer/containerimage/
-count=1` and `go test ./internal/storage/postgres/ -count=1` pass, including
the gate truth table, the handler signal forwarding proof, the single/batch
ACK emit-gate proofs, and the writer previous-last_set_id mapping proof. The
live suppression and activation-race proofs run against disposable Postgres
via `ESHU_POSTGRES_TEST_DSN`.

No-Observability-Change: no new instrument, label, span, or log field. The
verdict travels on the existing `Result.SubSignals` path (one new
`sub_signal_support_changed` key on the handler's existing sub-signal
logging); the suppressed completions are visible as a lower
`supply_chain_impact` execution rate against an unchanged identity rate.

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
- **An activation-epoch miss is classified, never failed blindly (#6502).**
  The epoch join returns no row for a pending, superseded, or missing
  generation alike, and the store reports that miss as
  `contract.ErrContainerImageIdentityGenerationNotActive`. Handle routes it
  through `gatedActivationEpoch` and the handler's `GenerationCheck`: a
  pending generation defers with `GenerationNotYetActiveError` (the queue
  retries without counting), a superseded one returns an early `superseded`
  `Result`, and a generation the check still calls current re-reads once
  before surfacing loudly. A nil check keeps the legacy loud error, so every
  production assembly must wire the check.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)

## Disposable identity-writer proofs

The unmapped-config live proof uses an external test wrapper to provide the
real PostgreSQL activation lookup. Its internal test-only helper keeps the
original handler, warning, legacy-row and tombstone assertions. The fixture
sets its active-generation pointer through the canonical trigger, which assigns
the epoch; it does not invent an epoch or copy the lookup query.

The package test binary can bootstrap an explicitly owned, empty database with
`-args -containerimage-bootstrap=1 -containerimage-database=NAME`. `NAME` must
start with `eshu_containerimage_` and match `ESHU_POSTGRES_DSN`. The connected
schema must be `public`, and the database must contain no user tables. The
bootstrap calls `postgres.ApplyBootstrap` with a two-minute context before any
test runs. Missing opt-in, identity mismatch, connection failure and a nonempty
database fail before execution. Ordinary test runs perform no automatic schema
bootstrap; their existing no-DSN skip behavior remains intact.

No-Regression Evidence: #7533 changes test fixtures only. The first real
15-test race run passed 14 cases and failed the unmapped-config fixture on its
missing activation lookup. After supplying the real lookup and canonical
activation, all 15 selected tests passed with no skips on a fresh PostgreSQL
18.6 database bootstrapped by this test binary. Package tests and the bootstrap
configuration/failure contracts passed. These are execution observations, not a
performance comparison. Production writers, SQL, pool sizes and concurrency
are unchanged.

No-Observability-Change: these test-only helpers add no runtime stage or
instrument. The handler's existing decision and retirement counters and reducer
execution signals remain unchanged. Bootstrap failures are printed before the
test binary exits nonzero.
