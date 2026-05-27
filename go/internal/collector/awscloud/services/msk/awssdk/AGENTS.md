# AGENTS.md - internal/collector/awscloud/services/msk/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - MSK SDK pagination, safe replicator description, and telemetry.
3. `mapper.go` - AWS SDK types to scanner-owned MSK record mapping.
4. `../scanner.go` - scanner-owned MSK fact selection.
5. `../README.md` - MSK scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep MSK SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe cluster, configuration, and replicator metadata.
- Do not call DescribeConfigurationRevision; the raw server.properties body
  is out of scope.
- Do not call GetBootstrapBrokers; broker endpoints are out of scope.
- Do not call ListScramSecrets, BatchAssociateScramSecret, or
  BatchDisassociateScramSecret; SCRAM secret material is out of scope.
- Do not call GetClusterPolicy, PutClusterPolicy, or DeleteClusterPolicy;
  resource policy JSON is out of scope.
- Do not call cluster, configuration, replicator, broker, or topic mutation
  APIs.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new MSK metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend replicator mapping only for AWS source data that is metadata and does
  not reveal Kafka topic data, message contents, broker logs, configuration
  revision bodies, bootstrap broker endpoints, or SCRAM secrets.

## What Not To Change Without An ADR

- Do not mutate MSK clusters, configurations, replicators, or topics.
- Do not introduce direct topic, partition, message, broker-log, or
  bootstrap-broker reads.
- Do not infer workload, environment, deployment, or ownership truth from
  cluster names, configuration names, replicator names, tags, or kafka
  cluster aliases.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
