# AGENTS.md - internal/collector/awscloud/services/stepfunctions/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Step Functions SDK pagination, definition projection, and
   telemetry.
3. `../scanner.go` - scanner-owned Step Functions fact selection.
4. `../README.md` - Step Functions scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Step Functions SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe state machine metadata, activity metadata, role ARNs,
  tags, structural state graph nodes, and Task Resource ARNs.
- Do not return the raw state machine definition string or literal
  Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result contents
  to the scanner.
- Do not call StartExecution, StopExecution, CreateStateMachine,
  UpdateStateMachine, DeleteStateMachine, SendTaskSuccess, SendTaskFailure,
  GetActivityTask, PublishStateMachineVersion, alias mutation APIs, or any
  other mutation or execution-payload API.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Step Functions metadata read by extending `Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend the definition projection only for AWS source data that is
  structural metadata and does not reveal payload contents, secrets, or
  business logic literals.

## What Not To Change Without An ADR

- Do not start executions, stop executions, mutate state machines, send to
  activities, or read execution history payloads.
- Do not infer workload, environment, deployment, or ownership truth from
  state machine names, activity names, tags, or definition contents.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
