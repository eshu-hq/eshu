# AGENTS.md - internal/collector/awscloud/services/mq/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - MQ SDK pagination, safe broker description, and the narrow
   `apiClient` interface.
3. `mapper.go` - AWS SDK types to scanner-owned MQ record mapping.
4. `../scanner.go` - scanner-owned MQ fact selection.
5. `../README.md` - MQ scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Amazon MQ SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface narrow: ListBrokers, DescribeBroker, and
  ListConfigurations only. `TestAPIClientNeverIncludesForbiddenMethods` fails
  the build if a mutation, reboot, DescribeUser, or
  DescribeConfigurationRevision method appears on the interface.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Record broker usernames only; never read or persist broker user passwords.
  Do not call DescribeUser.
- Do not call DescribeConfigurationRevision; the configuration XML body is out
  of scope.
- Do not call broker, configuration, or user mutation APIs, RebootBroker, or
  tag mutation APIs.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Amazon MQ metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend broker or configuration mapping only for AWS source data that is
  metadata and does not reveal broker user passwords, configuration XML bodies,
  or queue/topic message contents.

## What Not To Change Without An ADR

- Do not mutate Amazon MQ brokers, configurations, or users.
- Do not introduce broker user password reads, configuration XML body reads, or
  queue/topic message reads.
- Do not infer workload, environment, deployment, or ownership truth from broker
  names, configuration names, tags, or usernames.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
