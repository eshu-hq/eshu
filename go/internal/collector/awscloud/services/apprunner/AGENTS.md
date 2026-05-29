# AGENTS.md - internal/collector/awscloud/services/apprunner guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned App Runner domain types.
3. `scanner.go` - service, connection, autoscaling, observability,
   VPC-connector, VPC-ingress, and relationship emission.
4. `relationships.go` - relationship target-type and join-key construction.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - App Runner slice
   requirements.

## Invariants

- Keep App Runner API access behind `Client`; do not import the AWS SDK into
  this package.
- Keep the App Runner service `resource_id` equal to the service ARN. The ACM
  and WAFv2 scanners emit edges that target `aws_apprunner_service` by service
  ARN; changing the service identity form breaks those dangling edges.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from service names, image identifiers, repository URLs,
  or tags.
- Never read or persist runtime environment-variable values. Keep
  environment-variable NAMES only. The `Service` type must not declare a field
  that could carry a value, so a leak does not compile.
- Never read source repository credentials. Record only the connection ARN and
  access role ARN.
- Preserve runtime secret references as `value_from` ARN relationship edges and
  never attempt to resolve or read secret values.
- Every relationship must set a non-empty `target_type` matching the target
  scanner's `resource_id` form. Do not emit empty target types.
- Wrap client errors with `%w`; never swallow partial failures.
- Do not add a redaction key requirement. Environment values are dropped, not
  redacted, so the runtimebind registration leaves `RequiresRedactionKey` unset.

## Common Changes

- Add a new App Runner resource by extending the scanner-owned type, writing a
  focused scanner test first, then mapping it through `awscloud` envelope
  builders.
- Add new service or configuration fields only when the App Runner API reports
  them directly and the field is safe for persistence.
- Extend SDK pagination and describe enrichment in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not resolve App Runner services or images to source repositories here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not add an environment-variable value field or a source-credential field.
