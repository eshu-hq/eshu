# AGENTS.md — SBOM-attestation-attachment projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never attaches components to images.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after CI/CD run correlation and before service-catalog correlation.
5. `go/internal/reducer/sbom_attestation_attachment.go` for what the reducer
   does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildSBOMAttestationAttachmentReducerIntent` triggers only on a subject
  anchor — an `sbom.document`, `attestation.statement`, or OCI referrer fact —
  and anchors to the earliest such fact in original input order across the
  three kinds, via `FirstAcrossKinds`. Do not let component-only SBOM
  evidence trigger (components, dependency edges, external references, and
  warnings enrich the reducer decision only once a document-scoped intent
  exists; the focused test pins the no-trigger case) and do not introduce a
  per-kind priority (it changes `FactID` for generations that carry several
  kinds and the root fan-out parity fixture pins the current anchor).
- Keep the `sbom or attestation subject evidence observed` reason and the
  `sbom_attestation_attachment:<scope>` entity key byte-identical. The reducer
  claims one intent per scope generation and reloads the facts itself.
- `SourceSystem` is the shared `projectorintent.SourceSystem` two-tier
  fallback. Do not add a scope-level third tier here without a contract
  decision — the root file's private helper was body-identical to the shared
  one, which is why this family carries no local helper.
- Do not decode a payload or check a schema version here. This builder reads
  only `envelope.FactKind`; root projection already rejected unsupported
  schema versions.
- Do not attach documents or components to images, admit subject digests, or
  write to the graph here. The reducer's `DomainSBOMAttestationAttachment`
  handler owns all of that.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
