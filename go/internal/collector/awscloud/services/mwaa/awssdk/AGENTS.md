# AGENTS.md - internal/collector/awscloud/services/mwaa/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - MWAA SDK pagination, point reads, throttle handling, and
   telemetry.
3. `mapper.go` - safe metadata mapping and the Airflow-config exclusion.
4. `../scanner.go` - scanner-owned MWAA fact selection.
5. `../README.md` - MWAA scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep MWAA SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe environment identity, placement, and dependency metadata.
- Never map `AirflowConfigurationOptions`, `CeleryExecutorQueue`,
  `DatabaseVpcEndpointService`, `WebserverUrl`, or
  `WebserverVpcEndpointService` into scanner-owned types.
- Do not call any MWAA mutation, token (`CreateCliToken`,
  `CreateWebLoginToken`), REST-API (`InvokeRestApi`), metric-publishing
  (`PublishMetrics`), or tagging API. The `apiClient` interface must stay a
  List/Get read surface.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new MWAA metadata read only by extending `Client` (the scanner
  interface) and mapping the safe `GetEnvironment` response fields; write a
  scanner or adapter test first.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal configuration values, credentials, or endpoint secrets.

## What Not To Change Without An ADR

- Do not add a mutation, token, REST-API, metric-publishing, or tagging method
  to `apiClient`.
- Do not map Apache Airflow configuration option values or any secret-shaped
  field.
- Do not infer workload, environment, deployment, or ownership truth from MWAA
  names or tags.
- Do not write facts, graph rows, or reducer-owned state here.
