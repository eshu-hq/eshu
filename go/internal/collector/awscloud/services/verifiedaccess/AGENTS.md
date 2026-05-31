# AGENTS.md - internal/collector/awscloud/services/verifiedaccess guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Verified Access domain types.
3. `scanner.go` - instance, group, endpoint, and trust-provider resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and partition-aware ARN synthesis.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Verified Access API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read or persist trust-provider client secrets, OIDC client identifiers,
  group/endpoint policy documents, or any data-plane payload. Never call a
  Create/Modify/Delete mutation or a policy-read API.
- Verified Access is its own `service_kind` (`verifiedaccess`) with its own
  `ResourceType*` constants even though it ships under the EC2 SDK. Do not reuse
  the core `ec2` scanner's resource types.
- Instances, endpoints, and trust providers have no API ARN. Synthesize the
  partition-aware ARN with `awscloud.PartitionForBoundary`; never hardcode
  `arn:aws:`. Groups carry an API ARN and use it directly.
- Key the group-in-instance edge on the instance node's resource_id (synthesized
  instance ARN) and the endpoint-in-group edge on the group node's resource_id
  (API group ARN).
- Target the endpoint-to-subnet edge at `aws_ec2_subnet` by the bare subnet id
  and the endpoint-to-security-group edge at `aws_ec2_security_group` by the bare
  group id, matching the EC2 scanner's published node ids. Leave `target_arn`
  empty for those bare-id edges.
- Emit the endpoint-to-ACM-certificate edge only when AWS reports an ARN-shaped
  certificate, matching the ACM scanner's published resource_id.
- Record IAM Identity Center usage as an attribute, not an edge: AWS reports no
  IAM Identity Center instance ARN on the trust provider, so an edge would
  dangle.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from names or AWS tags.

## Common Changes

- Add a new Verified Access metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a secret or policy body,
  leave it out of the scanner contract.
- Add new relationship evidence only when the Verified Access API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read policy documents, trust-provider secrets, or any data plane, and
  do not call any Verified Access mutation API.
- Do not resolve Verified Access names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
