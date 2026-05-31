# AGENTS.md - internal/collector/awscloud/services/grafana guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Grafana domain types.
3. `scanner.go` - workspace resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware workspace ARN
   synthesis, and scanner-side cloning/dedup helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Managed Grafana API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read dashboards, panels, alert rules, or query results. Never read SAML
  or IAM Identity Center authentication configuration. Never call
  `DescribeWorkspaceAuthentication`, `CreateWorkspaceApiKey`, any
  service-account-token API, `AssociateLicense`, or any `Create*`, `Update*`,
  `Delete*` mutation API.
- The workspace node publishes its resource_id as the synthesized
  partition-aware workspace ARN (fallback to the bare workspace id). Source
  every outgoing edge on that exact value.
- Emit the workspace-to-IAM-role edge only when AWS reports an ARN-shaped
  `workspaceRoleArn`; the IAM scanner publishes a role resource_id as the role
  ARN, so set both `target_resource_id` and `target_arn` to that ARN.
- Emit the workspace-to-subnet and workspace-to-security-group edges only from a
  present `vpcConfiguration`. The EC2 scanner publishes subnet/security-group
  resource_ids as the BARE ids, so target the bare ids (no ARN) and de-duplicate
  repeated ids.
- Never hardcode `arn:aws:`. Synthesize the workspace ARN with
  `awscloud.PartitionForBoundary` so GovCloud and China resolve to the real
  partition.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Record data sources, notification destinations, and authentication providers
  as enum names only; never connection strings, endpoints, or secrets.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from workspace names or AWS
  tags.
- Keep workspace ARNs, names, endpoints, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Grafana metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry an authentication secret, API key,
  token, or data-plane payload, leave it out of the scanner contract.
- Add new relationship evidence only when the Grafana API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (role ARN for IAM roles, bare ids for VPC subnets and
  security groups).
- Extend SDK pagination and point reads in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read dashboards, alert rules, query results, or workspace
  authentication configuration; do not mint API keys or service-account tokens;
  do not mutate workspaces or associate licenses.
- Do not resolve Grafana names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
