# AGENTS.md - internal/collector/awscloud/services/sagemaker/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `exclusion_test.go` - the reflection gate proving InvokeEndpoint and every
   mutation are unreachable. Read it before touching `apiClient`.
3. `client.go` - the `apiClient` read surface, `NewClient`, tags, telemetry.
4. `client_endpoints.go`, `client_jobs.go`, `client_studio.go` - the List and
   bounded Describe reads.
5. `../scanner.go` - scanner-owned SageMaker fact selection.
6. `../README.md` - SageMaker scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep SageMaker SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- The `apiClient` interface holds only `List*`, `Describe*`, and `ListTags`.
  Never add an inference call (InvokeEndpoint / InvokeEndpointAsync) or any
  mutation. Never import `aws-sdk-go-v2/service/sagemakerruntime`.
- Wrap each AWS paginator page or point read in `recordAPICall` (via `page`).
- Keep metric labels bounded to service, account, region, operation, and
  result.
- `Describe*` reads copy only relationship-bearing fields. Never copy
  `HyperParameters`, data-config references, container `Environment`,
  lifecycle-config bodies, or pipeline definition bodies.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new SageMaker metadata read by extending `sagemaker.Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Keep new Describe fanout bounded and update the README performance note.

## What Not To Change Without An ADR

- Do not call InvokeEndpoint, InvokeEndpointAsync, or any mutation API.
- Do not import `sagemakerruntime`.
- Do not read or persist any forbidden payload (hyperparameters, data
  references, lifecycle-config bodies, container environment, pipeline
  definition body).
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
