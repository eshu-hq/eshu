# AWS Collector Catalog

## Purpose

The catalog package is the leaf that the #6696 AWS consolidation moves the
flat `constants_<service>.go` families into, grouped by service category
(`storage.go`, then one file per follow-up category). It exists because the
constants cannot live beside any service: the awscloud root uses the `Service*`
constants itself while every service package imports the root, so either
placement would be an import cycle. A leaf that imports neither side breaks
it.

## Ownership boundary

Owns the AWS service-kind, resource-type, and relationship string constants
only. It does not own scanners, runtime binding, redaction, envelopes, or
graph truth — those stay in `awscloud`, `awscloud/service/<service>`, and the
reducer. It must never import `awscloud`, any `awscloud/service` package, or
the AWS SDK: any of those reintroduces the cycle this package exists to
avoid.

## Exported surface

- `ServiceS3`, `ServiceEFS`, `ServiceFSx`, `ServiceStorageGateway`,
  `ServiceBackup` — storage-category service-kind strings
- `ResourceTypeS3Bucket`, `ResourceTypeEFS*`, `ResourceTypeFSx*`,
  `ResourceTypeStorageGateway*`, `ResourceTypeBackup*` — storage-category
  fact-schema resource-type tokens
- `RelationshipS3*`, `RelationshipEFS*`, `RelationshipFSx*`,
  `RelationshipStorageGateway*`, `RelationshipBackup*` — storage-category
  reducer relationship types
- `ServiceEC2`, `ServiceAutoScaling`, `ServiceLambda`, `ServiceECS`,
  `ServiceEKS`, `ServiceECR`, `ServiceBatch`, `ServiceLightsail` —
  compute-category service-kind strings
- `ResourceTypeEC2*`, `ResourceTypeAutoScaling*`, `ResourceTypeLambda*`,
  `ResourceTypeECS*`, `ResourceTypeEKS*`, `ResourceTypeECR*`,
  `ResourceTypeBatch*`, `ResourceTypeLightsail*` — compute-category
  fact-schema resource-type tokens
- `RelationshipEC2*`, `RelationshipAutoScaling*`, `RelationshipLambda*`,
  `RelationshipECS*`, `RelationshipEKS*`, `RelationshipBatch*`,
  `RelationshipLightsail*` — compute-category reducer relationship types
- `ServiceSNS`, `ServiceSQS`, `ServiceEventBridge`, `ServiceStepFunctions`,
  `ServiceMQ`, `ServiceMSK`, `ServiceSES`, `ServicePinpoint`,
  `ServiceFirehose` — messaging-category service-kind strings
- `ResourceTypeSNS*`, `ResourceTypeSQS*`, `ResourceTypeEventBridge*`,
  `ResourceTypeStepFunctions*`, `ResourceTypeMQ*`, `ResourceTypeMSK*`,
  `ResourceTypeSES*`, `ResourceTypePinpoint*`, `ResourceTypeFirehose*` —
  messaging-category fact-schema resource-type tokens
- `RelationshipSNS*`, `RelationshipSQS*`, `RelationshipEventBridge*`,
  `RelationshipStepFunctions*`, `RelationshipMQ*`, `RelationshipMSK*`,
  `RelationshipSES*`, `RelationshipPinpoint*`, `RelationshipFirehose*` —
  messaging-category reducer relationship types
- `ServiceAthena`, `ServiceEMR`, `ServiceGlue`, `ServiceKinesis`,
  `ServiceKinesisAnalyticsV2`, `ServiceOpenSearch`,
  `ServiceOpenSearchServerless` — analytics-category service-kind strings
  (query, processing, streaming, and search, in `analytics.go`)
- `ResourceTypeAthena*`, `ResourceTypeEMR*`, `ResourceTypeGlue*`,
  `ResourceTypeKinesis*`, `ResourceTypeManagedFlink*`,
  `ResourceTypeFirehose*`, `ResourceTypeSplunk*`,
  `ResourceTypeOpenSearch*` — analytics-category fact-schema resource-type
  tokens
- `RelationshipAthena*`, `RelationshipEMR*`, `RelationshipGlue*`,
  `RelationshipKinesis*`, `RelationshipManagedFlink*`,
  `RelationshipFirehoseDeliveryStream*`, `RelationshipOpenSearch*` —
  analytics-category reducer relationship types
- `ServiceQuickSight`, `ServiceCleanRooms`, `ServiceDatazone`,
  `ServiceDatabrew`, `ServiceDataSync` — analytics-category service-kind
  strings (BI, collaboration, governance, preparation, and movement, in
  `analytics_data.go`)
- `ResourceTypeQuickSight*`, `ResourceTypeCleanRooms*`,
  `ResourceTypeDatazone*`, `ResourceTypeDatabrew*`, `ResourceTypeDataSync*` —
  analytics-category fact-schema resource-type tokens
- `RelationshipQuickSight*`, `RelationshipCleanRooms*`,
  `RelationshipDatazone*`, `RelationshipDatabrew*`, `RelationshipDataSync*` —
  analytics-category reducer relationship types
- `ServiceSageMaker`, `ServiceBedrock` — analytics-category service-kind
  strings (machine learning, in `analytics_ml.go`)
- `ResourceTypeSageMaker*`, `ResourceTypeBedrock*` — analytics-category
  fact-schema resource-type tokens
- `RelationshipSageMaker*`, `RelationshipBedrock*` — analytics-category
  reducer relationship types
- `ServiceIAM`, `ServiceKMS`, `ServiceACM`, `ServiceACMPCA`,
  `ServiceSigner`, `ServiceSSOAdmin` — security-category service-kind
  strings (identity, access, and cryptography, in `security.go`)
- `ResourceTypeIAM*`, `ResourceTypeKMS*`, `ResourceTypeACMCertificate`,
  `ResourceTypeSigner*`, `ResourceTypeSSOAdmin*`, `IAMPolicySource*` —
  security-category fact-schema resource-type tokens
- `RelationshipIAM*`, `RelationshipKMS*`,
  `RelationshipACMCertificateUsedByResource`,
  `RelationshipACMPCA*`, `RelationshipSigner*`, `RelationshipSSOAdmin*` —
  security-category reducer relationship types
- `ServiceGuardDuty`, `ServiceSecurityHub`, `ServiceSecurityLake`,
  `ServiceDetective`, `ServiceInspector2`, `ServiceMacie`, `ServiceShield`,
  `ServiceFMS` — security-threat-category service-kind strings
  (detection and response, in `security_threat.go`)
- `ResourceTypeGuardDuty*`, `ResourceTypeSecurityHub*`,
  `ResourceTypeSecurityLake*`, `ResourceTypeDetective*`,
  `ResourceTypeInspector2*`, `ResourceTypeMacie*`, `ResourceTypeShield*`,
  `ResourceTypeFMSPolicy` — security-threat-category fact-schema
  resource-type tokens
- `RelationshipGuardDuty*`, `RelationshipSecurityHub*`,
  `RelationshipSecurityLake*`, `RelationshipDetective*`,
  `RelationshipInspector2*`, `RelationshipMacie*`,
  `RelationshipShield*`, `RelationshipFMS*` — security-threat-category
  reducer relationship types

See `doc.go` for the full contract. Each follow-up category lands as one more
`<category>.go` file with the same shape. The analytics category ships as
three files (`analytics.go` for query, processing, streaming, and search;
`analytics_data.go` for BI, collaboration, governance, preparation, and
movement; `analytics_ml.go` for machine learning) because one file would pass
the 500-line Go cap at ~989 lines. The security category ships as two files
(`security.go` for identity, access, and cryptography; `security_threat.go`
for detection and response) because one file would pass the 500-line Go cap;
all stay under it. The remaining `constants_*` files stay in the awscloud root
until their category lands.

## Dependencies

Almost none. The package imports only the standard library (tests only)
plus the published fact-schema contract (`sdk/go/factschema/aws/v1`) for
the eight EC2 resource-type tokens it already owned through that contract
(`compute.go` keeps the `awsv1.*` references verbatim, #6696). The contract
imports nothing back, so the leaf still cannot cycle. Anything that imports
the awscloud root or a service package cannot be imported here.

## Telemetry

None. String constants register no metrics, spans, or logs. The scanners
that consume these constants keep their existing counters; see
`docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

- A consolidation move must never change a value. `storage_test.go`,
  `compute_test.go`, `messaging_test.go`, `analytics_test.go`, and
  `security_test.go` pin every constant byte-identical to its
  pre-consolidation value; a red test there is a behavior change, not a move.

## Consolidation proof

No-Regression Evidence: the storage-category consolidation (Refs #6696)
moves 55 string constants from five `constants_<service>.go` root files into
`catalog/storage.go` verbatim (only the package clause changes); the 149
referencing `.go` files change only the qualifier (146 `awscloud.` to
`catalog.` plus the leaf import; 3 same-package test files qualify previously
bare identifiers). Baseline: the pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `storage_test.go` (55/55 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the full
affected suite (89 packages plus the awscloud root, awsruntime, and
`go/cmd/collector-aws-cloud`) is green before and after; `go build ./...` and
`go vet` are clean. Backend/version: none — a pure compile-time constant
relocation with no query, graph write, queue, lease, worker, batching, or
Compose/Helm change, so there are no row counts to regress; the input shape is
the fixed 55-symbol set. No-Observability-Change: the leaf declares no
instrument and runs no stage; scanner emission stays covered by
`eshu_dp_aws_resources_emitted_total` and `eshu_dp_aws_scan_duration_seconds`
(AWS cloud row in `docs/public/observability/telemetry-coverage.md`). Safe
because the Go compiler resolves every moved reference, the value-pinning test
fails on any drift, and the relguard/redaction disk-walk derivations were
extended to the catalog directory with their own failing-first tests.

No-Regression Evidence: the compute-category consolidation (Refs #6696)
moves 91 string constants from eight `constants_<service>.go` root files
(ec2, autoscaling, lambda, ecs, eks, ecr, batch, lightsail) into
`catalog/compute.go` verbatim (only the package clause changes; the eight
EC2 resource-type tokens keep their `awsv1.*` fact-schema references, and
the contract imports nothing back so the leaf still cannot cycle); the 196
referencing `.go` files change only the qualifier (`awscloud.` to
`catalog.`, plus the leaf import, dropping it where it goes unused) and
same-package root files qualify previously bare identifiers. Baseline: the
pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `compute_test.go` (91/91 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the
full affected suite (collector tree, reducer, coordinator, storage,
extensionconformance, `go/cmd/collector-aws-cloud`,
`go/cmd/webhook-listener`) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation test needed no walk
change: the storage slice already generalized it to every catalog category
file, and it passes with `compute.go` present. Backend/version: none — a
pure compile-time constant relocation with no query, graph write, queue,
lease, worker, batching, or Compose/Helm change, so there are no row counts
to regress. No-Observability-Change: the leaf declares no instrument and
runs no stage; scanner emission stays covered by
`eshu_dp_aws_resources_emitted_total` and `eshu_dp_aws_scan_duration_seconds`
(AWS cloud row in `docs/public/observability/telemetry-coverage.md`). Safe
because the Go compiler resolves every moved reference, the value-pinning
test fails on any drift, and the relguard/redaction disk-walk derivations
already cover the catalog directory.

No-Regression Evidence: the messaging-category consolidation (Refs #6696)
moves 64 string constants from nine `constants_<service>.go` root files
(sns, sqs, eventbridge, stepfunctions, mq, msk, ses, pinpoint, firehose)
into `catalog/messaging.go` verbatim (only the package clause changes); the 90
referencing `.go` files change only the qualifier (`awscloud.` to
`catalog.`, plus the leaf import). Baseline: the pre-move values read from
the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `messaging_test.go` (64/64 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the
full affected suite (collector tree, `go/cmd/collector-aws-cloud`,
`go/cmd/webhook-listener`) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation and relguard walks needed
no change: the storage slice already generalized them to every catalog
category file, and they pass with `messaging.go` present. Backend/version:
none — a pure compile-time constant relocation with no query, graph write,
queue, lease, worker, batching, or Compose/Helm change, so there are no row
counts to regress. No-Observability-Change: the leaf declares no instrument
and runs no stage; scanner emission stays covered by
`eshu_dp_aws_resources_emitted_total` and `eshu_dp_aws_scan_duration_seconds`
(AWS cloud row in `docs/public/observability/telemetry-coverage.md`). Safe
because the Go compiler resolves every moved reference, the value-pinning
test fails on any drift, and the relguard/redaction disk-walk derivations
already cover the catalog directory.

No-Regression Evidence: the analytics-category consolidation (Refs #6696)
moves 197 string constants from fourteen `constants_<service>.go` root files
(athena, emr, glue, kinesis, kinesisanalyticsv2, opensearch,
opensearchserverless, quicksight, cleanrooms, datazone, databrew, datasync,
sagemaker, bedrock) into `catalog/analytics.go`, `catalog/analytics_data.go`,
and `catalog/analytics_ml.go` verbatim (only the package clause changes); the
125 referencing `.go` files change only the qualifier (`awscloud.` to
`catalog.`, plus the leaf import, dropping it where it goes unused), plus one
shadowing parameter rename in the athena scanner and one doc-comment
qualifier. Baseline: the pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `analytics_test.go` (197/197 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the full
affected suite (collector tree, `go/cmd/collector-aws-cloud`) is green after
the move; `go build ./...` and `go vet ./...` are clean. The
redaction-derivation and relguard walks needed no change: the storage slice
already generalized them to every catalog category file, and they pass with
the analytics files present.

No-Regression Evidence: the security-category consolidation (Refs #6696)
moves 107 string constants from fourteen `constants_<service>.go` root files
(iam, kms, acm, acmpca, guardduty, securityhub, securitylake, detective,
inspector2, macie, shield, signer, ssoadmin, fms) into `catalog/security.go`
(identity, access, and cryptography) and `catalog/security_threat.go`
(detection and response) verbatim (only the package clause changes; the six
IAM resource-type tokens keep their `awsv1.*` fact-schema references, and
the contract imports nothing back so the leaf still cannot cycle). The split
is a size measure only: one file would pass the 500-line Go cap at ~589
lines, so the category ships as two files, both under the cap. The 244
referencing `.go` files change only the qualifier (`awscloud.` to
`catalog.`, plus the leaf import, dropping it where it goes unused);
same-package root files qualify previously bare identifiers, and two
symbol-name comments point at the new leaf. Baseline: the
pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `security_test.go` (107/107 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the
full affected suite (collector tree, reducer, coordinator, storage,
extensionconformance, `go/cmd/collector-aws-cloud`,
`go/cmd/webhook-listener`) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation and relguard walks needed
no change: the storage slice already generalized them to every catalog
category file, and they pass with `security.go` and `security_threat.go`
present. Backend/version for both: none — pure compile-time constant
relocations with no query, graph write, queue, lease, worker, batching, or
Compose/Helm change, so there are no row counts to regress.
No-Observability-Change (security slice): the leaf declares no instrument and runs no stage;
scanner emission stays covered by `eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` (AWS cloud row in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on any
drift, and the relguard/redaction disk-walk derivations already cover the
catalog directory.
- When a category carrying `Service*` constants lands, extend the
  `go/cmd/collector-aws-cloud` redaction-derivation test's constants walk to
  parse this directory too — it resolves every `runtimebind` `ServiceKind`
  through the parsed table and fails loudly on an unknown constant.
- Keep each category file under the 500-line Go cap; split an oversized
  category before it approaches the limit.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/reference/multi-cloud-collector-contract.md`
- Issue #6696 (consolidation checklist) and epic #6692 (target tree)
