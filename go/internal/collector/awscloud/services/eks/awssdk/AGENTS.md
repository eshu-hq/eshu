# AGENTS.md - internal/collector/awscloud/services/eks/awssdk guidance

## Read First

1. `README.md` - adapter purpose, telemetry, and invariants.
2. `client.go` - AWS SDK pagination and production client construction.
3. `mapper.go` - AWS SDK type to scanner-owned record mapping.
4. `oidc.go` - IAM OIDC provider lookup and issuer matching.
5. `telemetry.go` - AWS API call and throttle telemetry.
6. `../types.go` - scanner-owned records returned by the adapter.
7. `../scanner.go` - facts emitted from adapter output.

## Invariants

- Use AWS SDK paginators for EKS list operations that expose pagination.
- Call `DescribeCluster`, `DescribeNodegroup`, and `DescribeAddon` before
  returning scanner-owned records.
- Use IAM `ListOpenIDConnectProviders` and `GetOpenIDConnectProvider` to enrich
  EKS OIDC issuer URLs with provider ARN, thumbprints, and client IDs.
- Cache IAM OIDC provider lookup results per adapter instance to avoid repeated
  account-wide IAM reads for each cluster in the same claim.
- Emit AWS API call and throttle telemetry through `recordAPICall`.
- Do not persist kubeconfig data, bearer tokens, presigned URLs, or credential
  material.

## Common Changes

- Add a field to an EKS scanner-owned type in `../types.go`, then map the AWS
  SDK field in `mapper.go` and cover it in `client_test.go`.
- Add a new API operation only when the EKS service package has a fact or
  relationship that consumes the reported evidence.
- Keep IAM OIDC lookup bounded to provider metadata needed for IRSA trust-chain
  joins.

## What Not To Change Without An ADR

- Do not call Kubernetes APIs from this adapter.
- Do not infer workload, deployment environment, or ownership from EKS names,
  tags, or namespaces.
- Do not add write, mutate, or delete AWS APIs.
