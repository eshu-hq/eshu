# AGENTS.md — service-catalog-correlation projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that catalog facts are provenance only in the projector.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after SBOM attestation attachment and before the secrets/IAM trust
   chain.
5. `../schema_version_admission.go` for the root gate that rejects an
   unsupported service-catalog schema version before this builder runs.
6. `go/internal/reducer/servicecatalog/service_catalog_correlation.go` for
   what the reducer does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildServiceCatalogCorrelationReducerIntent` triggers on any fact kind the
  `facts.ServiceCatalogSchemaVersion` registry recognizes and anchors to the
  earliest such fact in original input order across every kind, via
  `FirstMatchingKindPredicate`. Do not replace the predicate with an explicit
  kind list (a kind added to the registry later would silently stop
  triggering) or introduce a per-kind priority (it changes `FactID` for
  generations that carry several kinds and the root fan-out parity fixture
  pins the current anchor).
- Keep the `service catalog facts observed` reason and the
  `service_catalog_correlation:<scope>` entity key byte-identical. The reducer
  claims one intent per scope generation and reloads the facts itself.
- Keep the three-tier source-system fallback (`SourceRef.SourceSystem`, then
  `CollectorKind`, then the scope's `SourceSystem`, each trimmed). Do not
  swap it for `projectorintent.SourceSystem`, which stops after the second
  tier; the focused test pins the third.
- Do not decode a payload or check a schema version here. This builder reads
  only `envelope.FactKind`; root projection already rejected unsupported
  schema versions, and its regression test stays at root.
- Do not correlate entities to repositories or services, decide ownership, or
  write to the graph here. The reducer's `DomainServiceCatalogCorrelation`
  handler owns all of that.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
