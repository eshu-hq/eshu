# AGENTS.md - internal/collector/awscloud/services/kinesis/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - the three narrow API interfaces, pagination, and telemetry.
3. `mapper.go` - AWS SDK types to scanner-owned record mapping.
4. `contract_test.go` - the reflection proof that forbidden APIs are
   unreachable. Treat it as the acceptance gate for issue #750.
5. `../scanner.go` - scanner-owned Kinesis fact selection.
6. `../README.md` - Kinesis scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector coverage.

## Invariants

- Keep Kinesis SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Reach AWS only through `dataStreamsAPI`, `firehoseAPI`, and `videoAPI`. Any
  new method added to those interfaces must be a metadata-only read and must
  keep `contract_test.go` green.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Data Streams: never call GetRecords, GetShardIterator, PutRecord, PutRecords,
  MergeShards, SplitShard, CreateStream, DeleteStream, UpdateStreamMode, or any
  stream-encryption or retention mutation.
- Firehose: never call CreateDeliveryStream, UpdateDestination,
  DeleteDeliveryStream, PutDeliveryStreamEncryptionConfiguration, PutRecord, or
  PutRecordBatch. Read only the `LambdaArn` processor parameter. Never map the
  HTTP endpoint access key, Splunk HEC token, Redshift username/password, or
  SecretsManager configuration.
- Kinesis Video: never call GetMedia, PutMedia, GetMediaForFragmentList,
  CreateStream, UpdateStream, DeleteStream, or UpdateDataRetention.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Kinesis metadata read by extending the relevant sub-API interface,
  updating `contract_test.go`'s `want` set, writing a scanner or adapter test
  first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend destination mapping only for AWS source data that is metadata and does
  not reveal records, media fragments, processing-configuration bodies, or
  destination secret material.

## What Not To Change Without An ADR

- Do not mutate data streams, delivery streams, or video streams.
- Do not introduce record-plane reads (GetRecords class) or media-plane reads
  (GetMedia class).
- Do not persist destination secret material or the processing Lambda body.
- Do not infer workload, environment, deployment, or ownership truth from
  stream names, delivery-stream names, tags, or endpoint URLs.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
