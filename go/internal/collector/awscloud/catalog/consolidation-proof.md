# Catalog consolidation proof

Per-lane move evidence for the #6696 `awscloud` constants consolidation.
Each entry records the baseline, the after measurement, and why the
change is safe; the `README.md` points here so the package doc stays
under the Markdown file cap.


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

No-Regression Evidence: the network-category consolidation (Refs #6696)
moves 162 string constants from fourteen `constants_<service>.go` root files
(vpc, vpclattice, route53, route53resolver, route53recoverycontrolconfig,
directconnect, transitgateway, networkmanager, networkfirewall, elb, elbv2,
globalaccelerator, servicediscovery, cloudfront) into `catalog/network.go`
(core fabric, DNS, and hybrid connectivity) and `catalog/network_edge.go`
(traffic delivery and service connectivity) verbatim (only the package clause
changes) — the split is a size measure only: one file would pass the 500-line
Go cap at ~825 lines, so the category ships as two files, both under the cap.
The 132 referencing `.go` files change only the qualifier (`awscloud.` to
`catalog.`, plus the leaf import, dropping it where it goes unused) and the
same-package `envelope_test.go` qualifies two previously bare identifiers.
Baseline: the pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `network_test.go` (162/162 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the
full affected suite (collector tree, `go/cmd/collector-aws-cloud`,
`go/cmd/webhook-listener`) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation and relguard walks needed
no change: the storage slice already generalized them to every catalog
category file, and they pass with `network.go` and `network_edge.go`
present. Backend/version: none — a pure compile-time constant relocation
with no query, graph write, queue, lease, worker, batching, or Compose/Helm
change, so there are no row counts to regress. No-Observability-Change: the
leaf declares no instrument and runs no stage; scanner emission stays covered
by `eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` (AWS cloud row in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on any
drift, and the relguard/redaction disk-walk derivations already cover the
catalog directory.

No-Regression Evidence: the database-category consolidation (Refs #6696)
moves 131 string constants from twelve `constants_<service>.go` root files
(rds, dynamodb, elasticache, redshift, memorydb, docdb, docdbelastic, neptune,
timestream, keyspaces, dax, lakeformation) into `catalog/database.go`
(relational, document, graph, and caching) and
`catalog/database_specialized.go` (time-series, wide-column, accelerator, and
lake governance) verbatim (only the package clause changes) — the split is a
size measure only: one file would pass the 500-line Go cap at ~632 lines, so
the category ships as two files, both under the cap. The 118 referencing `.go`
files change only the qualifier (`awscloud.` to `catalog.`, plus the leaf
import, dropping it where it goes unused, plus two planner files dropping the
root import entirely) and the same-package `posture_envelope_test.go`
qualifies previously bare identifiers. Baseline: the pre-move values read from
the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `database_test.go` (131/131 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the full
affected suite (collector tree) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation and relguard walks needed
no change: the storage slice already generalized them to every catalog
category file, and they pass with `database.go` and `database_specialized.go`
present. Backend/version for both: none — pure compile-time constant
relocations with no query, graph write, queue, lease, worker, batching, or
Compose/Helm change, so there are no row counts to regress.
No-Observability-Change (database slice): the leaf declares no instrument and runs no stage;
scanner emission stays covered by `eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` (AWS cloud row in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on any
drift, and the relguard/redaction disk-walk derivations already cover the
catalog directory.

No-Regression Evidence: the API-category consolidation (Refs #6696)
moves 114 string constants from ten `constants_<service>.go` root files
(apigateway, apigatewayv2, appsync, appconfig, apprunner, synthetics,
servicecatalog, proton, resiliencehub, fis) into `catalog/api.go` (API
gateways and application delivery) and `catalog/api_management.go`
(provisioning, testing, and resilience) verbatim (only the package clause
changes) — the split is a size measure only: one file would pass the
500-line Go cap at ~623 lines, so the category ships as two files, both
under the cap. The 88 referencing `.go` files change only the qualifier
(`awscloud.` to `catalog.`, plus the leaf import, dropping it where it goes
unused). Baseline: the pre-move values read from the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `api_test.go` (114/114 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the full
affected suite (collector tree) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation and relguard walks needed
no change: the storage slice already generalized them to every catalog
category file, and they pass with `api.go` and `api_management.go` present.
Backend/version for both: none — pure compile-time constant relocations with
no query, graph write, queue, lease, worker, batching, or Compose/Helm change,
so there are no row counts to regress.
No-Observability-Change (api slice): the leaf declares no instrument and runs no stage;
scanner emission stays covered by `eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` (AWS cloud row in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on any
drift, and the relguard/redaction disk-walk derivations already cover the
catalog directory.

No-Regression Evidence: the devtools-category consolidation (Refs #6696)
moves 174 string constants from twenty-two `constants_<service>.go` root
files (codeartifact, codebuild, codecommit, codedeploy, codeguru,
codepipeline, cloudformation, cloudtrail, config, controltower,
organizations, ram, servicequotas, resourcegroups, licensemanager,
cloudwatch, cloudwatchlogs, xray, applicationautoscaling, computeoptimizer,
mwaa, servicecatalogappregistry) into `catalog/devtools.go` (CI/CD),
`catalog/management.go` (governance and provisioning), and
`catalog/operations.go` (trail, metrics, tracing, scaling, and workflows)
verbatim (only the package clause changes) — the split is a size measure
only: one file would pass the 500-line Go cap at ~1071 lines, so the
category ships as three files, all under the cap. The 202 referencing `.go`
files change only the qualifier (`awscloud.` to `catalog.`, plus the leaf
import, dropping it where it goes unused, plus one controltower helper
dropping the root import entirely). Baseline: the pre-move values read from
the base tree
(`git show origin/main:go/internal/collector/awscloud/constants_<service>.go`)
are pinned in `devtools_test.go` (174/174 byte-identical,
`go test ./internal/collector/awscloud/catalog/ -count=1` green) and the full
affected suite (collector tree) is green after the move; `go build ./...` and
`go vet ./...` are clean. The redaction-derivation and relguard walks needed
no change: the storage slice already generalized them to every catalog
category file, and they pass with `devtools.go`, `management.go`, and
`operations.go` present. Backend/version for all three: none — pure
compile-time constant relocations with no query, graph write, queue, lease,
worker, batching, or Compose/Helm change, so there are no row counts to
regress.
No-Observability-Change (devtools slice): the leaf declares no instrument and runs no stage;
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
