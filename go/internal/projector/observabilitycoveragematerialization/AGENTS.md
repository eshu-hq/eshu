# AGENTS.md — observability-coverage-materialization projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../observabilitycoverage/AGENTS.md` — the sibling correlation half of the
   same #391 pair, which shares the `observabilityResourceTypes` set.
5. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the AWS-cloud-image probe and before the
   observability-coverage-correlation probe.
6. `go/internal/reducer/obscoverage/observability_coverage_materialization.go`
   for what the reducer does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- The trigger has ONE branch: an `aws_resource` fact whose decoded
  `resource_type` is in the `observabilityResourceTypes` closed set. An
  observability source fact is NOT a trigger here — that is the sibling
  correlation family's second branch, and conflating the two would enqueue
  materialization work for generations that cannot produce a `COVERS` edge.
- `observabilityResourceTypes` is this package's copy of a three-way mirror:
  the sibling correlation family
  (`../observabilitycoverage/correlation_intents.go`) keeps the same set and
  both mirror the reducer's `observabilityResourceSignals`
  (`go/internal/reducer/obscoverage/observability_coverage_correlation_index.go`).
  A resource type added to one copy must be added to all three.
- The entity key is `aws_resource_materialization:<scope>` — deliberately the
  SHARED key, not a family-distinct one, so the handler's readiness gate
  resolves the same `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS
  node builders publish. Changing it silently un-gates the COVERS edge write.
- The source label is the shared two-tier `projectorintent.SourceSystem`. This
  family has no literal third tier (the sibling does); do not "harmonise" them.
- An undecodable `aws_resource` payload is not a trigger and not an error: the
  decode error is swallowed to an empty resource type, which never matches the
  closed set. Root quarantines the invalid fact separately.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Adding an AWS observability resource type.** Update all three mirror
  copies (this package, the sibling correlation family, the reducer's signals
  map) in the same change, plus their tests.
  `TestBuildObservabilityCoverageMaterializationReducerIntentAcceptsEveryObservabilityType`
  enumerates the set literally here and must be extended with it.
- **Changing the reason string or the entity key.** Both are asserted verbatim
  by this package's tests AND by the root ordered fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`, entries in both
  `fanOutParityExpectations` and `fanOutParityExpectedOrder`); change them
  together.

## Failure modes

- **Path citations outside this package.** The `go/internal/mcp`
  route-serves-data registry
  (`go/internal/mcp/route_serves_data_registry_routes.go`) cites projector and
  query source files BY PATH and reads them for a marker string, so a
  projector-only rename can fail a test in `internal/mcp` with
  `read <path>: no such file` and nothing in projector pointing at it. Checked
  at extraction time: that registry cites the observability-coverage domains'
  evidence in `go/internal/query/*` files only, no projector path, and
  `go test ./internal/mcp/ -run TestRouteServesDataRegistry` stayed green. It
  is still worth a fresh `rg -n 'observability_coverage'` over
  `go/internal/mcp`, `sdk/go/factschema`, and `docs/internal/design` before
  committing a rename here.
- **`docs/internal/**` is scanned by no gate that resolves symbol names.**
  This family's file path WAS cited by
  `docs/internal/design/4786-contract-integration-matrix.md` (three rows) and
  was repointed with the extraction.
- **`scripts/verify-doc-citations.sh` refuses any byte edit to a Markdown row
  that carries a `go/internal/**.go:<line>` citation**, even when only a
  different citation on that row changes: authority is bound to the exact
  containing-line bytes. Repointing a path on such a row therefore requires
  dropping the `:<line>` suffixes on that row to file anchors (the form the
  gate itself recommends) and then reconciling with
  `bash scripts/verify-doc-citations.sh -update`.
- **The decode seam file name is load-bearing.** The payload-usage manifest
  gate (`scripts/verify-payload-usage-manifest.sh`) discovers
  `factschema_decode_aws.go` by its `factschema_decode*.go` name and the
  `factschema.FactKindAWSResource` reference in its body; renaming the file or
  inlining the decode hides the seam from the gate. The wrapper's own name
  must stay unique across seam files.
- **`docs/public/observability/telemetry-coverage.md` has a stale-target
  check.** Every non-test `.go` file added under `go/internal/projector` needs
  a row, and a row whose path no longer exists fails the gate — root's
  `factschema_decode_aws.go` row was removed with the file.
- **Root keeps the `buildProjection` dispatch tests.**
  `../observability_coverage_materialization_intents_test.go` stays at root:
  one case asserts BOTH observability domains are absent from an input-invalid
  generation, and its `observabilityAWSResourceEnvelope` fixture is shared
  with `../scope_generation_intents_fanout_test.go`. Do not move it here.

## Anti-patterns

- Do not widen the trigger to observability source facts to "match the
  sibling". The narrower trigger is the point: no AWS observability object
  means no `COVERS` edge.
- Do not give this family its own entity key for tidiness; the shared key is
  the readiness gate.
- Do not widen the export surface past
  `BuildObservabilityCoverageMaterializationReducerIntent`. Every sibling
  family in this series exports exactly one builder and no types.
- Do not "deduplicate" the `observabilityResourceTypes` copy into a shared
  package in passing; the three-way mirror is a deliberate design decision
  recorded in `docs/internal/design/package-restructure.md`.

## Changes needing ADR review

- Changing `reducer.DomainObservabilityCoverageMaterialization`, the trigger
  branch, the `aws_resource_materialization:<scope>` entity key, or the
  source-system label. All are contract surface the reducer handler and the
  fan-out parity fixture assert against.
- Consolidating the `observabilityResourceTypes` three-way mirror into a
  shared home.
- Adding a second decode seam or reading any payload field beyond
  `resource_type`.

## Verification

Use TDD. Run the focused package tests, the root dispatcher fan-out and
probe-count tests, package-doc verification, the projector package tree, the
payload-usage manifest gate, dirgate, and the golden-corpus gates selected by
the changed paths.
