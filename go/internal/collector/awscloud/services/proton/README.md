# Proton scanner

`package proton` is the AWS cloud collector scanner for AWS Proton. It reports
control-plane metadata for Proton environments, services, environment templates,
and service templates, and the relationships that join them to each other and to
IAM roles.

## What it emits

Resources:

- `aws_proton_environment` — identity, environment template name and version,
  provisioning mode, deployment status, reported Proton service-role ARN, and the
  provisioning account id (for cross-account environments).
- `aws_proton_service` — identity, service template name, status, and the source
  repository linkage (branch, repository id, CodeStar connection ARN) **by
  reference only**.
- `aws_proton_environment_template` — identity, display name, provisioning mode,
  recommended version.
- `aws_proton_service_template` — identity, display name, pipeline provisioning
  mode, recommended version.

Relationships:

- `proton_service_in_environment` — `service -> environment`, an internal edge
  derived from Proton service instances and keyed by the environment ARN the
  environment node publishes. Repeated instances of one service in one
  environment collapse to a single edge. A placement that names an environment
  the scanner did not observe (for example a cross-account environment) is
  skipped rather than dangled.
- `proton_environment_uses_role` — `environment -> aws_iam_role`, keyed by the
  reported `ProtonServiceRoleArn`, which is the role ARN the IAM scanner
  publishes as its role `resource_id`. A non-ARN role identifier emits no edge.

## Metadata-only contract

The scanner never reads or persists:

- service or environment **spec manifest bodies** (the `Spec` field on
  `GetService` output is intentionally never mapped),
- pipeline **spec bodies**,
- template version **schema bodies** (the scanner never calls a template-version
  reader),
- deployment **input parameter values** or service-instance specs (only the
  service-name/environment-name join keys are kept from `ListServiceInstances`).

Synthesized identifiers are partition-aware: the scanner keys IAM-role and
environment edges by the ARNs AWS already reports, so commercial, GovCloud, and
China partitions all join the real node. No `arn:aws:` literal is ever
hardcoded.

## SDK read surface

The accepted SDK surface (`awssdk/client.go` `apiClient`) is exactly
`ListEnvironments`, `ListServices`, `GetService`, `ListEnvironmentTemplates`,
`ListServiceTemplates`, `ListServiceInstances`, and `ListTagsForResource`. Every
mutation, sync-status/config reader, deployment-output reader, and
provisioned-resource reader is excluded by construction, proven by the reflection
guard in `awssdk/exclusion_test.go`.

## Skipped edge

The spec mentions a template-to-S3-template-bundle-bucket edge "if reported." No
Proton **read** API exposes the template bundle S3 bucket on its output (the S3
bundle source appears only in create-time `TemplateVersionSourceInput`), so this
scanner emits no such edge rather than keying a dangling one.

## Performance and observability

No-Regression Evidence: metadata-only control-plane scanner; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/proton/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Verification

```bash
cd go
go test ./internal/collector/awscloud/services/proton/... -count=1
go test ./internal/collector/awscloud/ -run 'ServiceKind' -count=1
golangci-lint run ./internal/collector/awscloud/...
```
