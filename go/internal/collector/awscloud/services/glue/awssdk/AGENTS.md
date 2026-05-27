# AGENTS.md - internal/collector/awscloud/services/glue/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Glue SDK pagination, safe metadata mapping, and telemetry.
3. `../scanner.go` - scanner-owned Glue fact selection.
4. `../README.md` - Glue scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Glue SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe database, table, crawler, job, trigger, workflow, and
  connection metadata.
- Always set `HidePassword=true` on `GetConnections` so AWS never returns
  password property values.
- Always set `IncludeGraph=false` on `GetWorkflow` so workflow graph and run
  state stay outside the scanner contract.
- Forward only argument and connection-property key names. Never propagate
  argument or connection-property values.
- Do not call any Glue mutation API, classifier custom-pattern read, column
  statistics read with sample values, or user-defined function read.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Glue metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal payload content, credentials, or sample row content.

## What Not To Change Without An ADR

- Do not run jobs, start crawlers, mutate Data Catalog state, read column
  statistics with sample values, or call any Glue mutation API.
- Do not call `GetConnections` with `HidePassword=false`.
- Do not call `GetWorkflow` with `IncludeGraph=true`.
- Do not infer workload, environment, deployment, or ownership truth from
  Glue names, parameters, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
