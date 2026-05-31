# AGENTS.md - internal/collector/awscloud/services/firehose/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Firehose SDK pagination, the describe fan-out, and telemetry.
3. `mapping.go` - safe metadata mapping from the SDK delivery stream
   description into scanner-owned types.
4. `../scanner.go` - scanner-owned Firehose fact selection.
5. `../README.md` - Firehose scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Firehose SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface to `ListDeliveryStreams` and
  `DescribeDeliveryStream` only. Never add PutRecord, PutRecordBatch, any
  Create/Update/Delete, Start/StopDeliveryStreamEncryption, or
  Tag/UntagDeliveryStream. The exclusion reflection test must stay green.
- Wrap each AWS list page or describe point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe delivery stream metadata: identity, status, stream type,
  source type and source Kinesis stream ARN, encryption mode/status and
  customer-managed KMS key ARN, creation time, and the join-relevant destination
  identities plus transform Lambda ARNs.
- Map the KMS key ARN only for `CUSTOMER_MANAGED_CMK`.
- Never map a Splunk HEC token, an HTTP endpoint URL or access key, a Redshift
  password, or any processing-configuration body. Read only the `LambdaArn`
  processor parameter for transform edges.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Firehose metadata read by extending the scanner `Client` and this
  adapter, writing a scanner or adapter test first, then mapping the SDK
  response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend destination mapping only for AWS source data that is metadata and does
  not reveal record payloads, credentials, or endpoint secrets.

## What Not To Change Without An ADR

- Do not put records, mutate streams, toggle encryption, tag streams, or call
  any Firehose mutation API.
- Do not widen the `apiClient` interface beyond the two read operations.
- Do not infer workload, environment, deployment, or ownership truth from
  Firehose names, destinations, or tags.
- Do not write facts, graph rows, or reducer-owned state here.
