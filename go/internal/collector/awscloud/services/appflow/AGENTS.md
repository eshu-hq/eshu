# AGENTS.md - internal/collector/awscloud/services/appflow guidance

## Read First

1. `README.md` - package purpose, exported surface, metadata-only policy, and
   invariants.
2. `types.go` - scanner-owned AppFlow domain types.
3. `scanner.go` - flow, connector profile, and relationship emission.
4. `relationships.go` - relationship emission rules and join-key shapes.
5. `helpers.go` - partition-aware S3 ARN synthesis and Secrets Manager ARN
   matching.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AppFlow API access behind `Client`; do not import the AWS SDK into this
  package.
- Never start, stop, or run flows, and never wire any `Create*`, `Update*`,
  `Delete*`, `StartFlow`, or `StopFlow` API.
- Never read flow run records (`DescribeFlowExecutionRecords`).
- Never read or persist field mappings (the flow's task transforms). They can
  encode literal transferred data values. The scanner-owned `Flow` type has no
  field that can hold them.
- Never read or persist connector credentials or OAuth tokens. The only
  credential reference allowed is the connector profile's Secrets Manager
  credentials ARN, recorded solely to drive the profile-to-secret edge.
- Source flow outgoing edges on the same id the flow node publishes as its
  resource_id (the flow ARN, or the flow name when the ARN is absent).
- Key the connector profile node by its name; flows reference connector profiles
  by name, so the flow-to-connector-profile edge targets the name.
- Emit flow-to-S3 edges only when the connector is Amazon S3 and AWS reports a
  bucket name. Derive the synthesized bucket ARN partition from the flow ARN
  (or the boundary region when the flow ARN is absent) via
  `awscloud.PartitionFromARN` / `awscloud.PartitionForBoundary`. Never hardcode
  `arn:aws:`.
- Emit the flow-to-KMS-key edge only when AWS reports a customer KMS key ARN.
- Emit the connector-profile-to-secret edge only when the credentials ARN parses
  as a Secrets Manager ARN by exact service-segment match (parse the colon
  fields), never by `strings.Contains(":secretsmanager:")`.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from flow or connector
  profile names or AWS tags.
- Preserve stable flow and connector profile identities across repeated
  observations in the same AWS generation.
- Keep AppFlow resource ARNs, names, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new AppFlow metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry transferred data values or
  credential material, leave it out of the scanner contract.
- Add new relationship evidence only when the AppFlow API reports both sides
  directly and the target identity is not sensitive (an ARN or a stable name).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not run, start, stop, or mutate flows or connector profiles, or call any
  AppFlow mutation API.
- Do not call `DescribeFlowExecutionRecords` or any API that returns flow run
  records.
- Do not read or persist field mappings, connector credentials, or OAuth
  tokens.
- Do not persist AppFlow tags into facts until tagging is added through an ADR
  that documents the source API and label policy.
- Do not resolve AppFlow names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
