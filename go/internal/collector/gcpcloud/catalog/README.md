# GCP Collector Catalog

## Purpose

The catalog package is the leaf that the #6696 GCP consolidation moves the
flat `extractor_<type>.go` constant families into, grouped by family
(`gcp_compute_core.go`, `gcp_compute_lb.go`, `gcp_compute_net.go` for lane H;
`gcp_security.go` for lane K). It exists because the constants cannot stay
beside the extractors without staying package-private to the whole gcpcloud
root: any subpackage that needs one family's strings (a future grouped
extractor, a test helper) would either duplicate them or import the root and
close an import cycle. A leaf that imports neither side breaks it.

HK opener: this note records the lane HK move of the 34 compute+security
extractor files' package-private constant families (147 declarations:
H=100 across 19 compute files, K=47 across 15 security files including the
ungrouped API Gateway file) into this leaf as `gcp_compute_core.go`,
`gcp_compute_lb.go`, `gcp_compute_net.go`, and `gcp_security.go` (#6696),
verbatim apart from the export. The three-way compute split is a size measure
only: one file would pass the 500-line Go cap, so the lane ships as three
files (all under the cap).

No-Regression Evidence (gcp-hk slice): the consolidation moves declarations
only — every value is pinned byte-identical by
`TestHKConstantsMatchGCPCloudValues` (146 identifiers covering 147 source
declarations; the function-local `versionsMarker` folds into the identical
`SecretVersionVersionsMarker`) in `catalog/gcp_hk_test.go`, and `go test`
across the gcpcloud tree plus `go build ./...` are green after the repoint
(referencing files change only the qualifier to `catalog.`, plus the leaf
import). Baseline: the pre-move values read from the base tree for each of
the 34 root files via Go's own parser. Backend/version: none — pure
compile-time constant relocations with no query, graph write, queue, lease,
worker, batching, or Compose/Helm change, so there are no row counts to
regress.

No-Observability-Change (gcp-hk slice): the leaf declares no instrument and
runs no stage; extractor emission stays covered by
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` (gcp extractor rows in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on any
drift, and the per-extractor `TestExtract*` suites keep asserting the same
emitted strings.

## Ownership boundary

Owns the compute- and security-family CAI asset-type, relationship-type,
full-resource-name-prefix, and path-marker string constants only. It does not
own extractors, the extractor registry, claim boundaries, redaction,
envelopes, or graph truth — those stay in `gcpcloud`, `gcpcloud/extractor`,
and the reducer. It must never import `gcpcloud`, `gcpcloud/extractor`, any
extractor group package, or a cloud SDK: any of those reintroduces the cycle
this package exists to avoid.

## Exported surface

- `AssetTypeCompute*` — compute-family CAI asset-type tokens (in
  `gcp_compute_core.go`, `gcp_compute_lb.go`, `gcp_compute_net.go`)
- `RelationshipType*` — compute- and security-family bounded provider
  relationship types carried on `gcp_cloud_relationship` facts
- `PeeringStateActive`, `SslCertificateTypeSelfManaged`,
  `NetworkEndpointGroupServerless*` — compute-family bounded enum values
- `AssetTypeCertificateManager*`, `CertManagerCertificateType*`,
  `RelationshipTypeCertManager*` — Certificate Manager family (in
  `gcp_security.go`)
- `AssetTypeKMSKeyRing`, `RelationshipTypeCryptoKeyInKeyRing`,
  `KmsCryptoKeyPurposeEncryptDecrypt`, `KmsKeyRing*Segment` — KMS family
- `OrgPolicyAssetType`, `OrgPolicyFullResourceNamePrefix`,
  `OrgPolicyFullResourcePoliciesMarker`,
  `RelationshipTypeOrgPolicyAppliesToResource`,
  `AssetTypeCloudResourceManager*` — Organization Policy family
- `SecretVersionAssetType`, `AssetTypeKMSCryptoKeyVersion`,
  `RelationshipTypeSecretVersion*`, `SecretVersionVersionsMarker` — Secret
  Manager Secret Version family
- `ServiceAccountKeyAssetType`, `RelationshipTypeServiceAccountKeyOf`,
  `ServiceAccountKeyKeysMarker` — Service Account Key family
- `WorkloadIdentityPoolAssetType`, `WorkloadIdentityPoolProviderAssetType`,
  `RelationshipTypeWIFProviderOfPool`, `WifProvidersMarker` — Workload
  Identity family
- `ApiKeyAssetType`, `IamRoleAssetType`, `IdentityConfigAssetType`,
  `RecaptchaKeyAssetType` — single-asset families
- `AssetTypeAPIGateway*`, `RelationshipTypeAPIGatewayUsesAPIConfig`,
  `ApiGatewayAPIConfig*`, `ApiGatewayLocationsMarker` — API Gateway family
- `AwsDefaultAttributeMappingKeyCount` — Workload Identity Pool provider
  default attribute-mapping key count (the only int constant)

See `doc.go` for the full godoc contract.

## Dependencies

None. This package imports only the Go standard library — by construction it
has no dependency to list, which is what keeps the leaf acyclic.

## Telemetry

- Metrics: none. This leaf declares no instrument and runs no collection
  stage; extraction outcomes remain covered by
  `eshu_dp_gcp_cloud_attribute_extractions_total` and
  `eshu_dp_gcp_cloud_facts_emitted_total` (see
  `docs/public/observability/telemetry-coverage.md`, gcp extractor rows).

## Gotchas / invariants

- The export rule is mechanical: uppercase the first letter, preserve
  everything else. `ApiKeyAssetType` (not `APIKeyAssetType`) and
  `WifProvidersMarker` (not `WIFProvidersMarker`) are intentional — each
  catalog identifier maps back to exactly one pre-consolidation gcpcloud
  identifier by that trivial transform.
- One source declaration folds: the function-local `versionsMarker` in
  `extractor_secret_version.go` carries the identical `"/versions/"` value as
  the package-level `secretVersionVersionsMarker`, so its uses bind to
  `SecretVersionVersionsMarker` after the move. The pin test documents this.
- Function-local markers with no package-level twin mint one catalog
  identifier each (`ApiGatewayLocationsMarker`, `KmsKeyRingLocationsSegment`,
  `KmsKeyRingKeyRingsSegment`).
- Keep every file under 500 lines; split a family file before the cap (lane H
  already ships as three files for this reason).
- Keep the SPDX header on every new file.
- Cross-group constants already owned by `gcpcloud/extractor` (KMS,
  Compute, Storage, Pub/Sub topic, GKE cluster asset types and their
  prefixes) stay there; this leaf does not redeclare them.

## No-Regression Evidence (gcp-ij slice): the IJ data+platform consolidation
moves the data-family and platform-family string constants from the 33 flat
`extractor_*.go` root files into `catalog/gcp_data.go` and
`catalog/gcp_platform.go` verbatim (only the package clause and identifier
case change) — the split is a size measure only: one file would exceed the
500-line Go cap, so the slice ships as two family leaves, both under the
cap. Baseline: the pre-move values read from the base tree
(`git show origin/main:go/internal/collector/gcpcloud/extractor_*.go`) are
pinned in `gcp_ij_test.go`
(`TestIJConstantsMatchGCPCloudValues`,
`go test ./internal/collector/gcpcloud/catalog/ -count=1` green) and the
full affected suite (`gcpcloud` tree plus the reducer compat retirement
test) is green after the move; `go build ./...` is clean. Backend/version
for both: none — pure compile-time constant relocations with no query,
graph write, queue, lease, worker, batching, or Compose/Helm change, so
there are no row counts to regress.
No-Observability-Change (gcp-ij slice): the leaf declares no instrument and
runs no stage; extraction and emission stay covered by
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` (collector gcp rows in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on
any drift, and the catalog leaf holds only string constants.

## Related docs

- `docs/public/reference/gcp-cloud-collector-contract.md`
- `../AGENTS.md` — the gcpcloud source-fact contract and invariants this
  leaf serves
- `../extractor/AGENTS.md` — the sibling extractor-leaf precedent (wave 1)
