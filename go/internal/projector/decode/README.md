# Fact decode seam

## Purpose

The projector's single seam between an untyped fact payload and the typed
factschema values the canonical extractors build rows from — plus the
quarantine path that decides what happens when a payload will not decode.

## Ownership boundary

This package is a **leaf**: it imports no other projector package, which is
what lets `../canonical`, `../stage` and `../runtime` all depend on it. It
owns decoding and classification only; it builds no rows, writes nothing, and
holds no queue or retry concern.

Two things live here that their names might not suggest:

- `payload.go` — the untyped accessors. Every caller moved out of the
  projector root with the packages that use them, so the accessors followed
  the callers rather than staying behind at the root.
- `fact_kind.go` — `NormalizeFactKind` and the `Filter*Facts` selectors. These
  are fact-kind selection, not a stage, even though they arrived as
  `stage_facts.go`. `../canonical` uses them as heavily as `../stage` does, so
  keeping them in `stage` would have made `canonical -> stage -> runtime ->
  canonical` a cycle.

## Exported surface

- Typed decoders, one per family: `CodegraphRepository`, `CodegraphFile`,
  `OCIRegistryRepository`, `OCIImageManifest`, `OCIImageIndex`,
  `PackageRegistryPackage`, `TerraformStateSnapshot`, `TerraformStateResource`
  and their siblings.
- Untyped payload accessors: `PayloadString`, `PayloadInt`, `PayloadIntPtr`,
  `PayloadBoolPtr`, `PayloadHasKey`, `PayloadAttributes`.
- Fact-kind selection: `NormalizeFactKind`, `FilterFileFacts`,
  `FilterEntityFacts`, `FilterRepositoryFacts`.
- Quarantine and admission: `QuarantinedFact`, `PartitionFailures`,
  `GroupQuarantinedFactsByStage`, `QuarantinedFactStage`,
  `RecordQuarantinedFacts`, `Error`, `NewError`, `ValidateFactSchemaVersion`.
- The bounded canonical stage labels in `stage_label.go`.

## Behavior worth knowing before changing anything here

`PartitionFailures` is the whole point of the package. A decode failure caused
by an absent required payload field is `input_invalid`: it comes back as a
`QuarantinedFact` and the caller dead-letters it. **Every other** decode error
is returned fatally. There is no third branch, and neither branch may become a
silent skip — a swallowed decode failure produces a graph that is quietly
missing rows, which is the accuracy failure the Life Motto ranks first.

The stage labels are a bounded metric dimension. Adding an unbounded value
makes `eshu_dp_projector_input_invalid_facts_total` unusable.

See `doc.go` for the full godoc contract.
