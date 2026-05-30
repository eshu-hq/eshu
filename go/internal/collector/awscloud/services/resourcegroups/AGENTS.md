# AGENTS.md - internal/collector/awscloud/services/resourcegroups guidance

## Read First

1. `README.md` - package purpose, exported surface, membership classifier table,
   and invariants.
2. `types.go` - scanner-owned Resource Groups domain types.
3. `helpers.go` - the ARN-to-family membership classifier and ARN parsing.
4. `scanner.go` - group resource and relationship emission.
5. `relationships.go` - membership and stack-backing relationship rules.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `../../internal/relguard/README.md` - the graph-join contract the membership
   edges must satisfy.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Resource Groups API access behind `Client`; do not import the AWS SDK
  into this package.
- Never call a mutation API (`CreateGroup`, `UpdateGroup`, `DeleteGroup`,
  `UpdateGroupQuery`, `GroupResources`, `UngroupResources`, `Tag`, `Untag`,
  `PutGroupConfiguration`). The `Client` interface excludes them by
  construction, proven by the reflection guard in `scanner_test.go`.
- Never persist the resource-query body. Record the query type only; the only
  query field kept is the stack identifier (an ARN identity) of a
  CloudFormation-stack-backed group.
- Classify member ARNs by parsing their colon-separated fields, never by
  substring-matching the raw ARN. Match the ARN `service` segment exactly.
- Emit a membership edge only when `classifyMember` recognizes the family.
  Unrecognized families return `ok=false` and are skipped; never fall back to
  `aws_resource` or an empty `target_type`.
- The published `target_resource_id` for each family MUST match that family's own
  scanner. Before adding a family, READ the target scanner's `scanner.go` to
  confirm whether it publishes the ARN, a bare id, or a prefixed id, and set the
  ARN-keyed flag accordingly. ARN-keyed targets set `target_arn`; bare/prefixed
  targets do not.
- Never synthesize an ARN with a hardcoded `arn:aws:` partition. Member ARNs come
  from the API; use them directly so GovCloud and China joins resolve.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from group names or AWS tags.
- Keep group ARNs, names, query bodies, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new member family by extending `classifyMember` (and `classifyEC2Member`
  for EC2 sub-types). Write the classifier test case first, READ the target
  scanner's `resource_id` shape, then map it exactly. Use `arnTarget` for
  ARN-keyed families and `bareTarget` for bare/prefixed-id families.
- Add a new group metadata field by extending `Group`, writing a focused scanner
  or adapter test first, then mapping it through the `awscloud` envelope
  builders. If the field can carry query-body or tag content, leave it out of
  the contract until an ADR documents a sanitized exception.
- Extend SDK pagination and the query-body parse in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not call any Resource Groups mutation API or `PutGroupConfiguration`.
- Do not persist the resource-query body (tag filters, CloudFormation template
  JSON) or group tags into facts.
- Do not add an `aws_resource` / empty-`target_type` fallback for unrecognized
  member families; skipping is the contract.
- Do not resolve group names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
