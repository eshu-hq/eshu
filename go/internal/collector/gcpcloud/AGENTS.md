# AGENTS.md - internal/collector/gcpcloud guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `docs/public/reference/gcp-cloud-collector-contract.md` - the provider source,
   scope/generation, fact family, payload boundary, telemetry, and fixture
   contract.
3. `docs/public/reference/multi-cloud-collector-contract.md` - the shared
   cloud-collector boundary this package inherits.
4. `types.go` - `CollectorKind`, `Boundary`, `ParentScopeKind`,
   `ResourceObservation`, `WarningObservation`.
5. `normalize.go` - CAI full-resource-name and ancestor normalization.
6. `redaction.go` - `RedactionPolicyVersion`, label, IAM-member, and DNS value
   fingerprinting.
7. `parse.go` - safe CAI page parsing (drops the raw resource data blob).
8. `envelope.go` - durable fact-envelope construction and validation.
9. `relationship.go` - GCP relationship source fact construction and support
   states.
10. `image_reference.go` - image-reference fact construction and container-name
   fingerprinting.
11. `generation.go` - generation accumulation, dedupe, and fencing.
12. `metrics.go` - scoped OTEL instruments with bounded labels.
13. `extractor.go` - the per-asset-type typed-depth extractor registry
   (`RegisterAssetExtractor`, `AttributeExtraction`, `ExtractContext`).
The per-asset-type typed-depth extractors (`extractor_<type>.go`) are
catalogued in two sibling files. Read the relevant one before adding or
modifying an extractor:

- [`AGENTS-extractors-1.md`](AGENTS-extractors-1.md) — catalog items 14–46:
  `extractor_bigquery_table.go` (item 14, the reference extractor to copy for a
  new asset type) through `extractor_spanner_instance.go`.
- [`AGENTS-extractors-2.md`](AGENTS-extractors-2.md) — catalog items 47–61:
  `extractor_bigquery_transfer_config.go` through
  `extractor_spanner_database.go`. Append a new extractor's entry at the end
  of part 2 and keep the numbering ascending.

Both catalog files are one continuous ascending list numbered from 14. This
split also fixed a pre-existing duplicate `45.` (two entries shared it), which
renumbered every item from `extractor_spanner_instance.go` (now #46) onward up
by one — so `extractor_bigquery_transfer_config.go` moved from #46 to #47 and
`extractor_cloud_build_trigger.go` from #59 to #60. An older issue or review
note that cites one of those shifted numbers points one entry too low; resolve
it by extractor filename, not by the stale number. The only in-catalog
"#N above" back-references (`#28`, `#38`) sit below the shift and are unchanged,
and "#PR" references are PR numbers rather than list positions, so both remain
valid.

## Invariants

- GCP cloud data is reported source evidence. This package may emit typed source
  facts for parsed resources, provider relationships, label-backed tag
  observations, IAM policy observations, DNS record observations,
  image-reference observations, and collection warnings. Do not materialize
  graph truth, reducer admission, or query behavior here.
- Keep the claim boundary explicit: collector instance, parent scope kind and id,
  asset family, content family, location bucket, scope id, generation id, and a
  positive fencing token.
- Preserve the CAI full resource name verbatim. Add normalized asset type,
  project id/number, folder/org ancestors, and location alongside it; never
  replace raw identity.
- Keep stable fact keys deterministic from fact kind, full resource name, asset
  type, content family, and provider update time. Duplicate delivery within a
  generation must converge; stale generations are rejected by fencing token via
  `GenerationTracker`.
- Never put secrets, IAM policy JSON, object contents, startup scripts, public or
  private IP addresses, raw provider response bodies, or the raw CAI resource
  data blob in facts. The parser is the single redaction choke point for the data
  blob.
- Typed depth is per-asset-type: register one `AssetAttributeExtractor` per asset
  type in its own `extractor_<type>.go` file via `init()`; never grow a shared
  parser switch. An extractor receives the raw resource.data transiently and
  returns only bounded, redaction-safe attributes, correlation anchors, and typed
  relationships. Drop data-plane locators (object paths inside source URIs,
  request bodies); keep only resource identities (bucket, dataset, KMS key names).
  Adding a new asset type's attributes does not bump the `gcp_cloud_resource`
  schema version — the `attributes`/`correlation_anchors` fields are generic.
- Fingerprint IAM members, DNS record values, and sensitive label values through
  the keyed `redact` package. Fingerprint container names before image-reference
  emission. Member class is a bounded enum; raw identities, DNS record values,
  and container names are never persisted.
- Keep payload redaction versioned with `RedactionPolicyVersion`.
- Metric labels are bounded enums only (collector kind, claim status, operation,
  parent scope kind, asset family, content family, status class, fact kind,
  warning kind, outcome). Never label-leak resource ids, project ids, names,
  labels, IAM members, DNS names, image references, URLs, or credential names.
- This package does not call Google Cloud APIs. A future runtime adapter owns SDK
  pagination, retries, throttling, and credential loading.

## Common Changes

- Add a new GCP fact family only after `internal/facts` exposes the fact kind and
  schema version via `GCPFactKinds()` / `GCPSchemaVersion(kind)` and registers it
  in `CoreFactKinds()`.
- Keep every source file under 500 lines; split into a sibling before the cap.
- Normalize a CMEK CryptoKey reference through the shared
  `cmekKeyFullResourceName` in `extractor_helpers.go`; do NOT copy the
  trim/`//`-guard/prefix logic into a new per-extractor helper. It is strict:
  a blank yields "", an absolute name passes through only for the Cloud KMS
  domain (any other `//service.googleapis.com/...` is rejected so a malformed
  asset can never mint a wrong-domain KMS edge), and a bare relative name is
  prefixed after a single leading-slash trim. An extractor needing extra shape
  validation wraps it (see `sqlInstanceKMSKeyFullName`). The Disk
  `kmsCryptoKeyFullName` self-link parser (strips a trailing
  `/cryptoKeyVersions/`) serves a different input contract and is intentionally
  separate. Use `dedupeSortedNonEmpty` (also in `extractor_helpers.go`) for a
  sorted-unique attribute slice rather than a bespoke set type.
- Update `README.md`, `doc.go`, and this file when the exported surface or
  contract changes. When adding or changing a typed-depth extractor, also update
  its entry in the `AGENTS-extractors-*.md` catalog (new extractors append to
  `AGENTS-extractors-2.md`). Then run `scripts/verify-package-docs.sh`.

### KMS/dedup helper consolidation evidence (#4400)

The #4400 consolidation of the per-extractor CMEK full-name and sorted-dedup
helpers into `extractor_helpers.go` is a pure refactor of in-process,
allocation-bounded string helpers on the collector's parse path; it changes no
query, graph write, worker, lease, batch, or Compose/Helm knob.

- No-Regression Evidence: baseline `go test ./internal/collector/gcpcloud
  ./internal/correlation/cloudinventory ./internal/relationships -count=1` = all
  pass before the change; after the change the same command = all pass with every
  pre-existing extraction/normalization assertion unchanged. Input shape:
  per-asset CAI `resource.data` blobs (bare relative, leading-slash, and
  already-`//cloudkms.googleapis.com/`-prefixed CMEK key names). The helpers are
  O(n) over a single reference string / bounded attribute slice with no added
  allocation versus the deleted per-file copies, so there is no hot-path cost
  delta on the git-collector E2E baseline. The only behavior delta is that a
  wrong-domain absolute CMEK reference — which real Cloud Asset Inventory never
  emits — is now dropped instead of surfaced, proven by the new
  `TestExtract*WrongDomainKMSKey*` extraction tests.
- No-Observability-Change: `extractor_helpers.go` registers no metric; extraction
  outcomes remain covered by the collector-local
  `eshu_dp_gcp_cloud_attribute_extractions_total` and
  `eshu_dp_gcp_cloud_facts_emitted_total` counters (see
  `docs/public/observability/telemetry-coverage.md`).

## What Not To Change Without An ADR

- Do not make this package call Google Cloud APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not introduce a generic `cloud_resource` source fact; GCP facts are
  provider-specific until a schema PR deliberately migrates AWS, GCP, and Azure
  together.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, labels, folders, or project aliases in this package.
```
