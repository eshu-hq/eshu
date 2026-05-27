# AGENTS.md - internal/collector/awscloud/services/stepfunctions guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Step Functions domain types.
3. `scanner.go` - state machine, activity, role, and referenced-resource fact
   emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Step Functions API access behind `Client`; do not import the AWS SDK
  into this package.
- Never call StartExecution, StopExecution, CreateStateMachine,
  UpdateStateMachine, DeleteStateMachine, SendTaskSuccess, SendTaskFailure, or
  any other mutation or execution-payload API.
- Never persist execution input, execution output, execution history events,
  or activity task tokens.
- Never persist the raw state machine definition string or literal
  Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result contents
  from a state machine definition. The state graph projection is restricted
  to state names, state types, structural transitions (Next, End, Default,
  choice and catch Next), and Task Resource ARNs.
- Never emit a referenced-resource relationship for a non-ARN Task Resource
  string such as `states:::lambda:invoke.waitForTaskToken`.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from state machine names, activity
  names, or tags.
- Preserve stable state machine and activity identities across repeated
  observations in the same AWS generation.
- Keep state machine ARNs, activity ARNs, role ARNs, tags, and definition
  contents out of metric labels.

## Common Changes

- Add a new Step Functions metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it
  through `awscloud` envelope builders.
- Add new relationship evidence only when the Step Functions API reports both
  sides directly and the target identity is an ARN.
- Extend the safe state graph projection only for structural fields that do
  not reveal payload contents, secrets, or business logic literals.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not start executions, stop executions, create/update/delete state
  machines, send to activities, or mutate any Step Functions resource.
- Do not resolve state machine names, activity names, tags, or definition
  contents into workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
