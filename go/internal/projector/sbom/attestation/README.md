# SBOM-attestation-attachment projector intents

## Purpose

This package recognizes SBOM and attestation subject-anchor facts for one
scope generation and builds the reducer intent that asks the reducer to attach
that supply-chain evidence — SBOM documents, attestation statements, and OCI
referrers — to canonical image subjects.

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
rejects unsupported SBOM-attestation schema versions before any builder runs,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainSBOMAttestationAttachment` handler owns subject-digest admission and
every attachment decision and write; the projector never attaches components
to images.

## Exported surface

- `BuildSBOMAttestationAttachmentReducerIntent` builds the
  `sbom_attestation_attachment` intent, anchored to the earliest
  `sbom.document`, `attestation.statement`, or OCI referrer fact in the
  generation's original input order.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the three
candidate fact-kind constants and `internal/reducer` for the domain constant.
There is no decode seam: like `package/source`, this builder reads only
`envelope.FactKind` and never a payload field.

## Telemetry

This package emits no signal directly. Root intent enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the reducer handler retains `eshu_dp_sbom_attestation_attachments_total`.

## Gotchas / invariants

- A subject anchor is required: an `sbom.document`, `attestation.statement`,
  or OCI referrer fact. Component-only SBOM evidence must never trigger —
  components, dependency edges, external references, and warnings enrich the
  reducer decision only once a document-scoped intent exists, and the focused
  test pins the component-only no-trigger case.
- No candidate kind outranks another: the anchor is the earliest candidate
  fact in original input order across the three kinds, via `FirstAcrossKinds`.
  Introducing a per-kind priority would change `FactID` for any generation
  that carries more than one kind and destabilize the reducer claim.
- The `Reason` is always `sbom or attestation subject evidence observed` and
  the `EntityKey` is `sbom_attestation_attachment:<scope>`, one intent per
  scope generation regardless of how many anchors it carries. The root
  fan-out parity fixture pins both; the reducer reloads the generation's
  facts itself.
- `SourceSystem` is the shared `projectorintent.SourceSystem` two-tier
  fallback (`SourceRef.SourceSystem`, then `CollectorKind`, each trimmed).
  The root file's private helper was compared body-for-body against the
  shared helper and found identical, so this family — unlike
  `servicecatalog`, whose helper carries a third scope-level tier — keeps no
  local copy.
- The payload is never decoded here, and the schema version is not checked
  here either. Root projection's `validateFactSchemaVersion` rejects an
  unsupported SBOM-attestation `schema_version` before this builder runs.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

## Related docs

- [Projector architecture](../../README.md)
- [Intent contract](../../intent/README.md)
- [Reducer domain catalog](../../../reducer/domain-catalog.md)
