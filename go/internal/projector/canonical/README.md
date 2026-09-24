# Canonical materialization

## Purpose

Turn one scope generation's facts into the canonical materialization the graph
writers persist. That is the source-local code graph — repository, directory,
file, entity, import, module, parameter, class-member and nested-function rows
— plus the typed OCI-registry, package-registry and Terraform-state row
families.

## Ownership boundary

This package owns row shape and extraction only. It reads the envelope slice
it is handed and nothing else: no store reads, no writes, no queue, no
retries, no telemetry, no transactions. The projector runtime
(`../runtime`) owns which extractors run, in what order, and what happens to
the rows afterwards. Typed decoding belongs to `../decode`, which this package
calls and never the reverse.

## Exported surface

- `BuildMaterialization` — the entry point: envelopes in, one
  `CanonicalMaterialization` plus the facts that had to be quarantined out.
- `ExtractEntityRows`, `ExtractOCIRegistryRows`, `ExtractPackageRegistryRows`,
  `ExtractTerraformStateRows` — the per-family extractors, callable on their
  own by the projection tests that assert one family's rows.
- `DropOversizedIndexKeys` and `MaxIndexedKeyBytes` — the write-side bound
  on indexed graph keys (#7058). `CanonicalNodeWriter.Write` calls it before
  building statements; it removes Module, entity, and Parameter rows whose
  indexed key is over 8000 UTF-8 bytes, plus the edge rows that reference them,
  and returns one `OversizedIndexKey` record per skipped node for the caller's
  metric and log. `MaxIndexedKeyBytes` is `graph.MaxIndexKeyBytes`. Every other
  label and indexed property (Terraform state, `kind` slots, entity metadata,
  semantic entities) is covered by the schema-derived statement guard that
  `storage/cypher.InstrumentedExecutor` runs on every graph write.
- `EntityMetadataFromPayload` — derives an entity's metadata map, preferring an
  explicit `entity_metadata` object and otherwise carrying through every
  non-structural payload key.
- The row types (`FileRow`, `EntityRow`, `DirectoryRow`, `ImportRow`,
  `ModuleRow`, `TerraformStateResourceRow`, `OCIImageManifestRow`,
  `PackageRegistryPackageRow` and their siblings) and the
  `TerraformStateOwnershipOutcome` vocabulary the tfstate writers read back.

## Two behaviors worth knowing before changing anything here

**Determinism.** The same generation must always produce the same
materialization, because the Ifá replay gate compares two runs of it. Anything
that makes extraction depend on map iteration order, wall-clock time, or store
state breaks that gate.

**Quarantine, never swallow.** A fact whose payload is missing a required
field comes back as a `decode.QuarantinedFact` so the caller can dead-letter
it. A field that is present but empty is a valid decode that the row builders'
own identity gate drops. Neither path may become a silent `continue`.

See `doc.go` for the full godoc contract.
