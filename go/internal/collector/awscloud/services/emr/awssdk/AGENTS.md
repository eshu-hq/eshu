# AGENTS.md - internal/collector/awscloud/services/emr/awssdk guidance

## Read First

1. `README.md` - adapter purpose, telemetry, and invariants.
2. `client.go` - AWS SDK pagination, enrichment, telemetry, and production
   client construction. The `emrAPIClient` and `emrServerlessAPIClient`
   interfaces here define the entire SDK surface the adapter may touch.
3. `mapper.go` - AWS SDK type to scanner-owned record mapping.
4. `../types.go` - scanner-owned records returned by the adapter.
5. `../scanner.go` and `../relationships.go` - facts emitted from adapter
   output.

## Invariants

- Bound `ListClusters` by state and a recent `CreatedAfter` window. Do not scan
  unbounded cluster history.
- Call `DescribeCluster` for full metadata, then `ListInstanceGroups` or
  `ListInstanceFleets` by `InstanceCollectionType`. Call `DescribeStudio` and
  `ListStudioSessionMappings` per studio, and `GetApplication` per Serverless
  application.
- Read security configurations name-only via `ListSecurityConfigurations`.
  Never call `DescribeSecurityConfiguration`.
- Use the `advance`/`advanceToken` helpers for pagination so an empty or
  unchanging continuation token terminates the loop.
- Emit AWS API call and throttle telemetry through `recordAPICall`. Wrap all
  errors with `%w`.
- Do not persist step command lines, bootstrap action script bodies, security
  configuration policy bodies, or EMR Serverless job-run entry-point arguments.

## Common Changes

- Add a field to an EMR scanner-owned type in `../types.go`, then map the AWS
  SDK field in `mapper.go` and cover it in `client_test.go`.
- Add a new API operation only when the EMR service package has a fact or
  relationship that consumes the reported evidence, and only if the operation
  is a metadata read.

## What Not To Change Without An ADR

- Do not add any mutation API, step-body reader (ListSteps, DescribeStep),
  bootstrap-body reader (ListBootstrapActions), security-config policy-body
  reader (DescribeSecurityConfiguration), or EMR Serverless job-run reader
  (GetJobRun, ListJobRuns, ListJobRunAttempts, GetSession, ListSessions) to the
  `emrAPIClient` or `emrServerlessAPIClient` interfaces. The exclusion tests
  fail if such a method name appears.
- Do not infer workload, deployment environment, or ownership from EMR names,
  tags, or roles.
- Do not synthesize ARNs or hardcode the AWS partition.
