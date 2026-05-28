# AGENTS.md - internal/collector/awscloud/services/transitgateway guidance

## Read First

1. `README.md` - package purpose, VPC pairing, and invariants.
2. `types.go` - scanner-owned Transit Gateway domain types and the read-only
   `Client` interface.
3. `scanner.go` - resource and relationship emission orchestration.
4. `observations.go` - per-resource attribute payload builders.
5. `relationships.go` - per-resource relationship builders, including the
   attachment resource-type dispatch and the cross-account peering edges.
6. `attributes.go` - option/info map builders and shared helpers.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `../vpc/README.md` - the VPC scanner this one pairs with. Read it before
   deciding whether a resource belongs here or in the VPC package.
9. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Transit Gateway API access behind `Client`; do not import the AWS SDK
  into this package.
- Never expose mutation operations on `Client`. Methods MUST be named `List*`.
  `scanner_test.go::TestClientInterfaceIsReadOnly` enforces this.
- Never read or persist transit gateway routes, multicast group memberships,
  or policy table rules. The `PolicyTable` type carries identity and state
  only.
- Surface cross-account peering attachments as AWS reports them. Emit the
  remote transit gateway identity, flag `cross_account`, and record the
  reported `owner_id`/`region`. Never resolve the remote account identity here;
  that is a downstream org-context join.
- Never emit `aws_vpc_route_table`, `aws_vpc_vpn_connection`, or any EC2-owned
  resource. Cross-package edges reference the owning scanner's type by
  `awscloud.ResourceTypeXxx`. `scanner_test.go::TestResourceTypesDisjointFromVPC`
  pins the boundary.
- Preserve stable transit gateway, route table, attachment, multicast domain,
  and policy table identities across repeated observations in the same AWS
  generation.
- Keep IDs, ARNs, tags, and CIDR-like strings out of metric labels.

## Common Changes

- Add a new Transit Gateway metadata field by extending the scanner-owned
  record in `types.go`, writing a focused scanner or adapter test first, then
  mapping it through `awscloud` envelope builders.
- Add new relationship evidence only when the AWS API reports both sides
  directly and the target type already exists (or you add a new
  `awscloud.RelationshipTransitGatewayXxx` constant alphabetically in
  `constants_transitgateway.go`).
- Extend SDK pagination, mutation guards, and the apiClient interface only in
  the `awssdk` adapter, never here.

## What Not To Change Without An ADR

- Do not add new resource ownership that overlaps with the VPC, EC2, or any
  other service scanner.
- Do not resolve cross-account peer identities or emit synthetic `aws_account`
  ownership facts; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
