# Extractor Shared Foundation

## Purpose

This leaf package holds the shared typed-depth foundation for the GCP cloud
collector's per-asset-type extractors: the init-time registry, the
extraction input/output shapes, the relationship observation and claim
boundary shapes, and the small string helpers extractors share. It exists so
extractor groups (starting with `serverless/`) can move out of the
`gcpcloud` root without creating an import cycle back to it, following the
AWS `catalog` leaf pattern from issue #6696.

## Ownership boundary

Owns the registry, the shared shapes, and the shared helpers. Does not own
parsing, normalization, redaction, envelope construction, generation
fencing, metrics, or transport — those stay in `gcpcloud` and `gcpruntime`.
Does not call Google Cloud APIs.

## Exported surface

- `RegisterAssetExtractor`, `LookupAssetExtractor`, `HasAssetExtractor`,
  `ExtractAssetAttributes`, `UnregisterAssetExtractorForTest` — the
  init-time registry and dispatch
- `AssetAttributeExtractor`, `ExtractContext`, `AttributeExtraction` — the
  extractor function shape and its input/output
- `RelationshipObservation`, `RelationshipSupport*` — the provider
  relationship observation shape and its bounded support states
- `Boundary`, `ParentScopeKind` (+ scope constants and `Valid`) — the claim
  scope-generation identity extractors carry on observations
- `CMEKKeyFullResourceName`, `DedupeNonEmpty`, `DedupeSortedNonEmpty`,
  `FirstNonEmpty`, `ImageDigestFromReference`, `NormalizeRFC3339` — the
  shared string helpers
- `AssetType*`, `CloudKMSResourceNamePrefix`,
  `StorageBucketResourceNamePrefixFmt` — the cross-group CAI asset types
  and resource-name prefixes

See `doc.go` for the full contract.

## Dependencies

Standard library only (`encoding/json`, `fmt`, `sort`, `strings`, `time`).
The parent `gcpcloud` package and every extractor group depend on this leaf;
nothing here depends on them.

## Telemetry

None. Extraction outcomes remain covered by the collector-local
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` counters, recorded by the
`gcpcloud` dispatch path.

## Gotchas / invariants

- Registration is init-driven and panics on blank, nil, or duplicate
  entries. A new extractor group package must be imported (even blank) by
  the collector binary or its extractors never register.
- The parent `gcpcloud` package aliases these symbols
  (`compat_extractor.go`) so unmoved callers keep compiling unchanged.
  Remove each alias once its last caller moves out of the root.
- Keep every file under 500 lines; split before the cap.

## Related docs

- `../AGENTS.md` — the gcpcloud source-fact contract and invariants
- `docs/public/reference/gcp-cloud-collector-contract.md`
