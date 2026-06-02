# AWS S3 Scanner

## Purpose

`internal/collector/awscloud/services/s3` owns the Amazon S3 scanner contract
for the AWS cloud collector. It converts bucket control-plane metadata into
`aws_resource` facts, emits a derived metadata-only `s3_bucket_posture` fact per
bucket, emits bounded `s3_external_principal_grant` facts for external
bucket-policy principals, and emits relationship evidence when S3 reports a
bucket server-access-log target bucket.

## Ownership boundary

This package owns scanner-level S3 fact selection and identity mapping. It does
not own AWS SDK pagination, STS credentials, workflow claims, fact persistence,
graph writes, reducer admission, or query behavior.

```mermaid
flowchart LR
  A["S3 API adapter"] --> B["Client"]
  B --> C["Scanner.Scan"]
  C --> D["aws_resource"]
  C --> G["s3_bucket_posture"]
  C --> H["s3_external_principal_grant"]
  C --> E["aws_relationship"]
  D --> F["facts.Envelope"]
  G --> F
  H --> F
  E --> F
```

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal S3 bucket metadata read surface consumed by `Scanner`.
- `Scanner` - emits bucket resources, derived `s3_bucket_posture` facts,
  bounded `s3_external_principal_grant` facts, and logging-target relationship
  facts for one boundary.
- `Bucket` - scanner-owned bucket representation with safe metadata only,
  including replication presence, policy-derived booleans, and bounded
  external-principal grant metadata (no raw policy).
- `ExternalPrincipalGrant` - scanner-owned metadata-only bucket-policy
  principal observation for public, cross-account, AWS service, and unsupported
  principal types.
- `Versioning`, `Encryption`, `PublicAccessBlock`, `Website`, `Logging`, and
  `Replication` - scanner-owned control-plane metadata groups.

## Dependencies

- `internal/collector/awscloud` for boundaries, resource constants,
  relationship constants, and envelope builders.
- `internal/facts` for emitted fact envelope kinds.

The package depends on a small `Client` interface rather than the AWS SDK for Go
v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts after `Scanner.Scan` returns.
The `awssdk` adapter records S3 API call counts, throttles, and pagination
spans.

## Gotchas / invariants

- S3 facts are metadata only. The scanner must not read objects, list object
  keys, mutate buckets, or persist object inventory.
- Bucket policy JSON, ACL grants, replication rules, lifecycle rules,
  notification configuration, inventory configuration, analytics configuration,
  and metrics configuration are not persisted.
- The `s3_bucket_posture` fact carries only derived booleans and safe
  identifiers/ARNs. `s3_external_principal_grant` carries only bounded
  principal kind, value, account, partition, service, outcome, and statement SID
  metadata. The SDK adapter reads the bucket policy document transiently
  (`GetBucketPolicy`) to derive the public-grant and cross-account-principal
  booleans plus external-principal metadata, then discards the raw document; the
  policy JSON and statement body never reach the scanner-owned `Bucket` model
  or any fact payload. Replication is reduced to a presence boolean
  (`GetBucketReplication`), not rule detail.
- Website configuration is reduced to status flags, redirect host, and routing
  rule count. Index and error document object keys are not persisted.
- Logging target grants and object-key format are not persisted. The scanner
  records only the target bucket and target prefix needed for relationship
  evidence.
- Tags are raw AWS tag evidence. Do not infer environment, owner, workload, or
  deployable-unit truth from tags in this package.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/s3/...`
covers the bounded S3 metadata path: regional paginated ListBuckets with
MaxBuckets set, HeadBucket,
GetBucketTagging, GetBucketVersioning, GetBucketEncryption,
GetPublicAccessBlock, GetBucketPolicyStatus, GetBucketOwnershipControls,
GetBucketWebsite, GetBucketLogging, GetBucketReplication, and GetBucketPolicy;
no object inventory calls, no persisted policy JSON, no ACL grant reads, no
mutations, and no graph writes in the collector. Per-bucket API fan-out is a
fixed, bounded set of control-plane describes (no N+1 against object inventory
or pagination per bucket).

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers S3 bucket metadata fact emission, logging-target relationship emission,
omission of object/policy/ACL/replication/lifecycle/notification fields, runtime
registration, command configuration, and the SDK adapter's safe metadata
mapping.

Collector Observability Evidence: S3 uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses S3 scans through `aws.service.scan`, `aws.service.pagination.page`,
API/throttle counters, resource/relationship counters, and `aws_scan_status`.

### Partition-aware bucket node identity (#862, keystone)

No-Regression Evidence: `go test ./internal/collector/awscloud/services/s3/... -count=1`
covers the new `TestBucketNodeIdentityDerivesPartition`,
`TestLoggingRelationshipDerivesPartition`, and `TestBucketARNDerivesPartition`
(commercial / `aws-us-gov` / `aws-cn` / blank-region-fallback) alongside the
existing commercial assertions in `scanner_test.go` / `awssdk/client_test.go`.
S3 buckets carry no API ARN, so the scanner synthesizes the node `ARN`,
`ResourceID`, ARN correlation anchor, and bucket->bucket logging endpoints; these
now derive the partition from the claim region (`partitionForRegion` in the SDK
adapter, `partition(boundary)` in the scanner) instead of hardcoding `aws`. This
is the keystone for the partition graph-join class: every partition-aware
consumer (Bedrock, CodePipeline, MQ, Config, SageMaker/Glue #859, Athena #861)
emits `arn:<partition>:s3:::<bucket>` targets and only resolves once the bucket
node carries the matching partition. Commercial output is byte-for-byte
unchanged; metadata-only, no graph-write or hot-path behavior change.

Node-identity note: in GovCloud/China deployments the bucket `CloudResource`
node identity (a uid input) changes from `arn:aws:s3:::<bucket>` to the
partition-correct ARN. Within a single-partition deployment there is no
migration — buckets are materialized with the correct ARN from the first scan.

No-Observability-Change: the fix only changes the partition substring of the
synthesized bucket ARN value; no instrument, span, metric label, or
`aws_scan_status` row changes.

Collector Deployment Evidence: S3 runs inside the existing hosted
`collector-aws-cloud` runtime, so `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` stay covered by the command wiring and Helm collector runtime.

### Partition-aware ARNs (#866)

No-Regression Evidence: `go test ./internal/collector/awscloud/services/s3/... -count=1`
keeps `TestBucketNodeIdentityDerivesPartition` and
`TestLoggingRelationshipDerivesPartition` green after the scanner and the SDK
adapter (`bucketARN`) were switched from their package-local `partition` /
`partitionForRegion` helpers to the shared `awscloud.PartitionForBoundary` and
`awscloud.PartitionForRegion`. The derivation logic is identical; commercial
output (`us-east-1`) is byte-for-byte unchanged; this is a metadata-only
consolidation with no graph-write, queue, or hot-path behavior change.

No-Observability-Change: the change only swaps helpers; the synthesized bucket
ARN value is unchanged for every region, and no instrument, span, metric label,
or `aws_scan_status` row changes.

### Derived bucket posture fact (#1144, PR1 facts-only)

No-Regression Evidence: `go test ./internal/collector/awscloud/services/s3/... ./internal/facts -count=1`
covers the new derived `s3_bucket_posture` fact (`TestScannerEmitsDerivedBucketPostureFact`,
`TestScannerPostureDerivesPartition`), the partition-aware envelope builder
(`TestNewS3BucketPostureEnvelope*`), the fact-kind registry
(`TestS3BucketPostureFactKindRegistry`), the SDK adapter's replication-presence
and transient policy reads (`TestClientListBucketsReadsSafeMetadataOnly`,
`TestClientListBucketsTreatsMissingOptionalBucketConfigAsEmptyMetadata`), and
the policy-derivation logic (`TestDeriveBucketPolicyFlags*`). The posture fact
carries only derived booleans and safe identifiers/ARNs; redaction guards assert
no policy JSON, ACL grants, statements, or object data reach the payload. This
is the facts-only slice: the scanner emits the new fact kind but no graph edge
is written. Reducer graph projection of this posture is a separate PR under
principal review. Metadata-only; no hot-path graph-write or queue behavior
change in this PR.

No-Observability-Change: the new fact flows through the existing AWS collector
`aws.service.scan` / `aws.service.pagination.page` spans and
`eshu_dp_aws_api_calls_total` (now also recording the `GetBucketReplication` and
`GetBucketPolicy` operations); no new instrument, metric label, or
`aws_scan_status` row is introduced, and no posture booleans, ARNs, or bucket
names enter metric labels.

### S3 external-principal grant fact (#1241, PR1 facts-only)

No-Regression Evidence: `go test ./internal/facts ./internal/collector/awscloud ./internal/collector/awscloud/services/s3/... -run 'S3ExternalPrincipal|ExternalPrincipal|DeriveBucketPolicyExternalPrincipal|DeriveBucketPolicyFlags|ScannerEmitsExternalPrincipal' -count=1`
covers the new `s3_external_principal_grant` fact registry, envelope builder
redaction guard, scanner emission, SDK adapter mapping, and transient
bucket-policy derivation for public wildcard, cross-account account ID,
cross-account ARN, AWS service principal, unsupported principal type,
URL-encoded documents, Deny statements, same-account principals, and malformed
documents. This is a facts-only slice: no graph writes, reducer projection, or
query behavior changes.

No-Observability-Change: external-principal grant facts reuse the existing S3
scanner and AWS collector telemetry. `GetBucketPolicy` is still one bounded
control-plane read per bucket, now deriving both posture booleans and grant
metadata from the same transient parse. No new metric, span, status row, metric
label, bucket name label, principal label, or raw policy label is introduced.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
