# AGENTS.md - internal/collector/awscloud/services/kinesis guidance

## Read First

1. `README.md` - scanner contract, telemetry, and invariants.
2. `scanner.go` - resource fact selection for the three sub-services.
3. `relationships.go` - KMS, IAM, Lambda, and destination edge selection.
4. `types.go` - scanner-owned metadata types and the `Client` interface.
5. `awssdk/README.md` - SDK adapter contract and the reflection proof.
6. `../../README.md` - AWS cloud envelope contract.
7. `docs/public/guides/collector-authoring.md` - collector authoring rules.

## Invariants

- One `service_kind` ("kinesis") covers Data Streams, Firehose, and Video
  Streams. The scanner rejects any other `service_kind`.
- Kinesis facts are metadata only. Never read stream records, never read video
  media fragments, never mutate any resource.
- The scanner-owned types carry no secret fields. Do not add a field for the
  Firehose processing Lambda body, HTTP endpoint access key, Splunk HEC token,
  or Redshift password/SecretsManager material.
- Emit KMS, IAM, Lambda, S3, and OpenSearch relationships only when AWS reports
  the ARN form for the target. Splunk and HTTP endpoints are keyed by URL; the
  Redshift edge is keyed by the JDBC-derived cluster identifier.
- Deduplicate the IAM role and transform Lambda edges per ARN.
- Do not call the AWS SDK from this package. SDK behavior belongs in `awssdk`.

## Common Changes

- Add a new metadata attribute by extending the relevant scanner-owned type and
  its observation builder, with a scanner test written first.
- Add a new relationship by adding a constant in
  `../../constants_kinesis.go`, extending the relationship builder, and proving
  positive and negative (non-ARN or missing-identity) cases.

## What Not To Change Without An ADR

- Do not emit record, media-fragment, or destination-secret material.
- Do not infer workload, environment, deployment, or ownership truth from
  stream names, delivery-stream names, tags, or endpoint URLs.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
- Do not split this scanner into three `service_kind` values; the single
  "kinesis" kind is the registered contract.
