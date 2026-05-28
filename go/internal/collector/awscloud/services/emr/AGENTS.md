# AGENTS.md - internal/collector/awscloud/services/emr guidance

## Read First

1. `README.md` - package purpose, emitted resources/relationships, and
   invariants.
2. `types.go` - scanner-owned EMR domain types. The `Client` interface here
   defines the entire SDK surface the scanner is allowed to touch.
3. `scanner.go` - cluster, instance group/fleet, security configuration,
   Serverless application, Studio, and session-mapping emission.
4. `relationships.go` - the network, IAM, security configuration, and KMS
   relationship edges with their join keys.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- Keep EMR API access behind `Client`; do not import the AWS SDK into this
  package.
- Never call any mutation API: RunJobFlow, TerminateJobFlows, AddJobFlowSteps,
  CancelSteps, AddInstanceGroups, AddInstanceFleet, ModifyInstanceGroups,
  ModifyInstanceFleet, ModifyCluster, SetTerminationProtection,
  SetKeepJobFlowAliveWhenNoSteps, SetVisibleToAllUsers, PutAutoScalingPolicy,
  RemoveAutoScalingPolicy, PutAutoTerminationPolicy, PutManagedScalingPolicy,
  PutBlockPublicAccessConfiguration, CreateSecurityConfiguration,
  DeleteSecurityConfiguration, Create/Delete/Update Studio,
  Create/Delete/Update StudioSessionMapping, AddTags, RemoveTags, and on the
  Serverless side Create/Delete/Update/Start/Stop Application, StartJobRun,
  CancelJobRun, TagResource, UntagResource.
- Never persist step command lines. Do not add ListSteps or DescribeStep to
  `Client`; `Args` must never appear on a scanner-owned type.
- Never persist bootstrap action script bodies. Do not add
  ListBootstrapActions.
- Never persist security configuration policy bodies. Read only
  ListSecurityConfigurations (name + creation time); never call
  DescribeSecurityConfiguration.
- Never persist EMR Serverless job-run entry-point arguments. Do not add
  GetJobRun, ListJobRuns, ListJobRunAttempts, GetSession, or ListSessions.
- Every relationship must set a non-empty `target_type` and a
  `target_resource_id` matching how the target scanner publishes its
  resource_id (VPC/subnet/SG bare ids, IAM role/profile and KMS ARN or name,
  security configuration name). Set `target_arn` only for ARN-shaped values;
  never synthesize an ARN or hardcode the partition.
- Emit reported evidence only. Do not infer workload ownership, environment,
  repository, or deployable-unit truth from cluster names, tags, or roles.
- Keep cluster ids, ARNs, role names, key ids, and tags out of metric labels.

## Common Changes

- Add a new safe EMR metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when EMR directly reports both sides as
  identity.
- Extend SDK pagination and mapping in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not add any mutation, step-body, bootstrap-body, security-config
  policy-body, or job-run reader to `Client`. The `awssdk` exclusion tests
  refuse to compile or fail if such a method name appears.
- Do not fabricate cluster-to-VPC or application-to-VPC edges; the EMR cluster
  and Serverless APIs do not report a VPC id. The VPC join is derived from
  subnet membership downstream.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
