# GCP Cloud Collector

## Purpose

`internal/collector/gcpcloud` owns the first GCP cloud collection slice. It
parses Cloud Asset Inventory (CAI) `assets.list` and `searchAllResources`
response pages into safe observations, normalizes Google Cloud identity, redacts
container names, sensitive label values, IAM member identities, and DNS record
values, and emits the `gcp_cloud_resource`, `gcp_cloud_relationship`,
`gcp_tag_observation`, `gcp_iam_policy_observation`, `gcp_dns_record`,
`gcp_image_reference`, and `gcp_collection_warning` source fact envelopes. From
the same IAM bindings it also emits the secrets/IAM mirror
(`gcp_iam_principal`, `gcp_iam_trust_policy`, `gcp_iam_permission_policy`) for
service-account grantees and ServiceAccount impersonation bindings, so the
reducer can correlate GCP IAM into the secrets/IAM read models (#2347/#2369).
Those facts carry only redaction-safe member fingerprints, target email digests,
bounded impersonation modes, role names, and resource identity; raw member
email, target service-account email, workload-pool subject, namespace, and
Kubernetes ServiceAccount names stay out of fact payloads.

This package does not call Google Cloud APIs, schedule collector runs, write
graph rows, persist raw provider payloads, or admit reducer truth. The
fixture-backed `gcpruntime.Source` scaffolding, shared
cloud inventory admission/readback, tag evidence admission, image identity
admission, relationship resolution, and IAM trust facts live in sibling packages
and reducers; production claimed-live wiring moved to the extracted satellite
(`eshu-hq/eshu-collector-gcp-cloud`, issue #8856). Claim-enabled scheduler activation, opt-in direct/effective tag
API evidence, and chart values are implemented. Issues #1997/#2644 record the
completed security-only sanitized smoke, while deployed-shape promotion proof
remains gated. See
[GCP Cloud Collector Contract](../../../../docs/public/reference/gcp-cloud-collector-contract.md).

## Collection flow

```mermaid
flowchart LR
    Pages["CAI assets.list / searchAllResources JSON (fixtures)"]
    Parse["ParseAssetsListPage / ParseSearchAllResourcesPage"]
    Obs["safe ResourceObservation (no data-plane content)"]
    Gen["Generation accumulator (dedupe, pagination resume)"]
    Env["NewCloudResourceEnvelope / NewCloudRelationshipEnvelope / NewTagObservationEnvelope / NewIAMPolicyObservationEnvelope / NewGCPTrustPolicyEnvelope / NewDNSRecordEnvelope / NewImageReferenceEnvelope / NewCollectionWarningEnvelope"]
    Tracker["GenerationTracker (fencing, stale rejection)"]
    Facts["gcp_cloud_resource / gcp_cloud_relationship / gcp_tag_observation / gcp_iam_policy_observation / gcp_dns_record / gcp_image_reference / gcp_collection_warning facts"]

    Pages --> Parse
    Parse -->|"raw resource data blob dropped"| Obs
    Obs --> Gen
    Gen --> Env
    Tracker -->|"reject stale fencing token"| Gen
    Env --> Facts
```

Only the source reader sees raw CAI payloads. Everything leaving this package is
a redacted fact, a bounded warning, or a normalized identity.

## Exported surface

- `CollectorKind` — durable `collector_kind` value `gcp`.
- `Boundary`, `ResourceObservation`, `WarningObservation` — claim and
  observation contracts.
- `ParentScopeKind` (`organization`, `folder`, `project`) with `Valid`.
- `ParseAssetsListPage`, `ParseSearchAllResourcesPage`, `AssetsListPage` —
  fixture-driven CAI parsing.
- `RegisterAssetExtractor`, `AssetAttributeExtractor`, `AttributeExtraction`,
  `ExtractContext` — the per-asset-type typed-depth extractor registry. Each
  supported asset type registers its extractor in its own file (see
  `extractor_bigquery_table.go` for the reference BigQuery Table extractor and
  `extractor_compute_network.go` for the VPC Network extractor);
  `ResourceObservation.Attributes` and `.CorrelationAnchors` carry the bounded,
  redaction-safe result into the `gcp_cloud_resource` fact.
- `AssetTypeFamily`, `LocationBucket`, `NormalizeAncestry`, `Ancestry`,
  `ProjectIDFromFullName` — normalization helpers.
- `FingerprintLabelValues`, `MemberClass`, `FingerprintMember`,
  `RedactionPolicyVersion` — redaction.
- `NewCloudResourceEnvelope`, `NewCollectionWarningEnvelope`,
  `NewCloudRelationshipEnvelope`, `RelationshipObservation`,
  `NewTagObservationEnvelope`, `TagObservation`,
  `NewIAMPolicyObservationEnvelope`, `IAMPolicyObservation`,
  `NewDNSRecordEnvelope`, `DNSRecordObservation`,
  `NewImageReferenceEnvelope`, `ImageReferenceObservation`,
  `ExtensionSchemaVersionDefault` — durable envelope construction.
- `Generation`, `NewGeneration`, `GenerationTracker`, `NewGenerationTracker`,
  `ErrStaleGeneration` — generation accumulation and fencing.
- Warning kinds and outcomes (`WarningKind*`, `Outcome*`) with `ValidWarningKind`
  and `ValidOutcome`.
- `Metrics`, `NewMetrics`, `ClaimStatus*` — scoped OTEL instruments with bounded
  labels.

## Invariants

- GCP cloud data is reported source evidence (`source_confidence=reported`). Do
  not materialize graph truth here.
- Preserve the CAI full resource name verbatim for exact reducer joins; add
  normalized fields alongside it.
- Stable fact keys derive from fact kind, full resource name, asset type, content
  family, and provider update time. Duplicate delivery converges; stale
  generations are rejected by fencing token.
- Never persist raw IAM policy JSON, secret values, object contents, startup
  scripts, environment variable values, public or private IP addresses, or
  provider response bodies. The parser drops the raw resource data blob.
- Fingerprint IAM member identities, DNS record names and targets, and
  sensitive label values with the keyed `redact` package; fingerprint container
  names before they leave image-reference observations; never persist raw user,
  group, service-account, DNS record value, or container-name text.
- ServiceAccount impersonation bindings on `iam.googleapis.com/ServiceAccount`
  resources emit `gcp_iam_trust_policy` instead of ordinary permission grants.
  The parser retains `resource.data.email` only for ServiceAccount resources and
  only long enough to compute the target member fingerprint and email digest.
- Keep the payload redaction versioned with `RedactionPolicyVersion`. Rotating
  the redaction key material MUST be accompanied by bumping
  `RedactionPolicyVersion`: the version stamp is what separates the pre- and
  post-rotation keyspaces, so fingerprints produced under different keys are
  never conflated as the same identity, and it signals that pre-rotation facts
  need re-fingerprinting (re-collection) to join the new keyspace. A key
  rotation without a version bump is unsupported.
- Metric labels and status keys are bounded enums only: collector kind, claim
  status, CAI operation, parent scope kind, asset family, content family, status
  class, fact kind, warning kind, and outcome. Never put full resource names,
  project ids, labels, IAM members, DNS names, image references, URLs, or
  credential names in labels.

## Verification

```bash
cd go && go test ./internal/collector/gcpcloud ./internal/facts -count=1
cd go && go build ./...
cd go && golangci-lint run ./internal/collector/gcpcloud/...
```
