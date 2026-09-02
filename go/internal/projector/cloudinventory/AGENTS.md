# AGENTS.md — cloud-inventory-admission projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the Azure relationship-materialization probe and before the
   workload-cloud-relationship probe.
5. `go/internal/reducer/cloud_inventory_admission.go` for what the reducer
   does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildCloudInventoryAdmissionReducerIntent` triggers only on a provider
  cloud-inventory source fact — `aws_resource`, `gcp_cloud_resource`, or
  `azure_cloud_resource` — and anchors to the earliest such fact in original
  input order across the three kinds, via `FirstMatchingKindPredicate`. Do not
  introduce a per-kind priority (it changes `FactID` for generations that
  carry more than one provider kind and the root fan-out parity fixture pins
  the current anchor).
- `cloudInventoryAdmissionSourceFactKinds` stays in lockstep with the
  fact-kind allowlist the reducer's admission evidence loader reads. Adding a
  provider kind here without the loader-side allowlist entry enqueues intents
  the handler cannot admit.
- Keep the `provider cloud-inventory source facts observed` reason and the
  `cloud_inventory_admission:<scope>` entity key byte-identical. The reducer
  claims one intent per scope generation and reloads the generation's facts
  itself.
- `SourceSystem` is the shared `projectorintent.SourceSystem` two-tier
  fallback. Do not add a third tier here without a contract decision — the
  root file's private helper was a pure delegation to the shared one, which is
  why this family carries no local helper.
- Do not decode a payload or check a schema version here. This builder reads
  only `envelope.FactKind`; root projection already rejected unsupported
  provider schema versions.
- Do not classify candidates, write identity rows, or serve the inventory
  readback here. The reducer's `DomainCloudInventoryAdmission` handler owns
  all of that.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Admitting another provider fact kind.** Add it to the trigger set alongside
  `facts.AWSResourceFactKind`, `facts.AzureCloudResourceFactKind`, and
  `facts.GCPCloudResourceFactKind`, then add the matching `routeReadEvidence`
  marker to `go/internal/mcp/route_serves_data_registry_routes.go` for
  `GET /api/v0/cloud/inventory`. The registry reads this file by path and
  greps for the marker, so a new kind that is not registered there serves a
  domain the registry cannot prove.
- **Changing the intent's reason or entity key.** Both are asserted verbatim by
  the package tests and read by reducer projection; change them together.

## Failure modes

- **The registry stops resolving this file.** It cites
  `go/internal/projector/cloudinventory/admission_intents.go` by path. Renaming
  or splitting this file breaks `TestRouteServesDataRegistryHonestStateGreen`
  from a distance, in `internal/mcp`, with a `read ...: no such file` error that
  does not name this package.
- **A provider fact with no source-system label.** `SourceSystem` resolves
  through the two-tier `projectorintent.SourceSystem`; a fact carrying neither
  `SourceRef.SourceSystem` nor `CollectorKind` yields an empty label rather than
  a defaulted one. That is intended here, unlike families that carry a literal
  third fallback.

## Anti-patterns

- Do not reintroduce a package-local `sourceSystem` forwarder. The root helper
  this package replaced was a bare one-line call to
  `projectorintent.SourceSystem`; re-adding it only hides the seam.
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past `BuildCloudInventoryAdmissionReducerIntent`.
  Every sibling family in this series exports exactly one builder and no types.

## Changes needing ADR review

- Adding a decode seam. This family triggers on fact presence alone and carries
  no typed-payload decode; families that need one keep a local decode call
  against `sdk/go/factschema` rather than importing root's wrapper, and that
  split is a design decision rather than a local call.
- Changing `reducer.DomainCloudInventoryAdmission` or the set of provider
  domains this route is documented to serve. Both are contract surface the
  route-serves-data registry asserts against.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
