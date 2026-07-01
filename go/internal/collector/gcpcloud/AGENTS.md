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
14. `extractor_bigquery_table.go` - reference extractor for
   `bigquery.googleapis.com/Table`; the model to copy for a new asset type.
15. `extractor_compute_network.go` - typed-depth extractor for
   `compute.googleapis.com/Network` (VPC) emitting contained-subnetwork and
   peering edges.
16. `extractor_bigquery_dataset.go` - typed-depth extractor for
   `bigquery.googleapis.com/Dataset` (location, expiration policies, default KMS
   key, and a redaction-safe access-ACL summary; KMS-key edge plus
   authorizes-view/dataset/routine edges).

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
- Update `README.md`, `doc.go`, and this file when the exported surface or
  contract changes, then run `scripts/verify-package-docs.sh`.

## What Not To Change Without An ADR

- Do not make this package call Google Cloud APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not introduce a generic `cloud_resource` source fact; GCP facts are
  provider-specific until a schema PR deliberately migrates AWS, GCP, and Azure
  together.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, labels, folders, or project aliases in this package.
```
