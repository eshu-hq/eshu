# sbomgenerator Agent Notes

- This package is a scanner-worker analyzer, not a replacement for the
  hosted `sbom-attestation` collector (`internal/collector/sbomruntime`).
  Do not let the two paths diverge: existing CycloneDX/SPDX/in-toto
  documents stay with the collector; generated SBOMs from a repository,
  image, or artifact target stay here.
- Scanner workers emit source facts only. Do not add reducer finding facts,
  graph projection ownership, or attachment truth here. The
  `reducer.BuildSBOMAttestationAttachmentDecisions` path is the only place
  that admits scanner-generated documents.
- Always emit at least one source or warning fact per completed claim. The
  analyzer guarantees a document fact plus a `no_components_found` warning
  when the inventory contributes zero components. Silent clean output is a
  contract violation that workflow will dead-letter at the scanner-worker
  boundary.
- Resource limits are part of the contract, not optional knobs. New limit
  classes must go in `scannerworker.ResourceLimits` and be enforced here
  before any fact is built. Do not partial-emit when a limit is exceeded.
- Failure payloads must stay bounded. Never include raw repository paths,
  image names, registry URLs, package coordinates, or `os.Args`. Map
  `Source` errors through `classifySourceError` and discard the cause
  string at the workflow boundary.
- Subject digests must use `sha256:<64 hex>`. Do not invent or normalize
  subjects beyond stripping whitespace and lowercasing valid digests.
  Reducers own the attached/unknown/ambiguous classification, not this
  package.
- Coverage rule: any new warning reason, fact payload key, or component
  field must come with a focused unit test and a reducer-side test that
  proves it still flows through `sbom_attestation_attachment` without
  short-circuiting truth.
