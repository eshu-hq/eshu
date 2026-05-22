# AWS Lambda Scanner

## Purpose

`internal/collector/awscloud/services/lambda` owns scanner-side Lambda fact
selection for the AWS cloud collector. It converts functions, aliases, event
source mappings, image references, VPC placement, and execution-role evidence
into `aws_resource` and `aws_relationship` facts.

The package implements the Lambda slice from
`docs/public/services/collector-aws-cloud.md`.

## Ownership boundary

This package owns scanner-owned Lambda models and fact-envelope construction.
It does not own AWS SDK calls, credentials, throttling, workflow claims, graph
writes, reducer admission, or query behavior.

## Exported surface

See `doc.go` for the godoc contract.

- `Scanner` - emits Lambda facts for one claimed AWS boundary.
- `Client` - scanner-owned read surface implemented by `awssdk.Client`.
- `Function`, `Alias`, and `EventSourceMapping` - scanner-owned Lambda records.
- `VPCConfig` and `LoggingConfig` - non-secret Lambda configuration blocks used
  as fact attributes.

## Dependencies

The scanner imports AWS collector boundaries, fact envelope builders, fact
envelope kinds, and `internal/redact` for keyed HMAC-SHA256 function
environment value markers. It depends on a scanner-owned `Client` port rather
than the AWS SDK.

## Telemetry

This scanner emits no metrics directly. The AWS SDK adapter records API calls
with shared AWS collector events, spans, throttle counters, and operation
labels.

## Gotchas / invariants

- Function environment values are always replaced with keyed HMAC-SHA256
  markers before fact emission.
- The AWS Lambda GetFunction API returns presigned package download URLs. Those
  URLs must not be persisted; only image URI, resolved image URI, and KMS
  metadata are safe scanner evidence.
- Lambda aliases and event-source mappings remain reported AWS evidence. They
  do not prove deployable-unit, workload, or ownership truth until reducer
  correlation admits them.
- VPC subnet and security group relationships are join evidence for EC2
  topology. The collector does not infer network exposure or environment.
- Resource names, ARNs, tags, event-source ARNs, and environment variable names
  must not become metric labels.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/reference/telemetry/index.md`
