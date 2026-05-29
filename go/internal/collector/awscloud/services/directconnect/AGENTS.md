# AGENTS.md - internal/collector/awscloud/services/directconnect guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Direct Connect domain types.
3. `scanner.go` - resource and relationship emission orchestration.
4. `relationships.go` - relationship target-type and join-key rules.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage.

## Invariants

- Keep Direct Connect API access behind `Client`; do not import the AWS SDK into
  this package.
- NEVER call a Direct Connect mutation API: Create/Delete/Update/Associate/
  Disassociate/Confirm/Allocate/Accept/Tag/Untag for connections, virtual
  interfaces, gateways, gateway associations, or LAGs. The adapter `apiClient`
  interface excludes all of them and a reflection test asserts the exclusion.
- NEVER read or persist the BGP authentication key (`authKey`) on a virtual
  interface or any BGP peer. The scanner-owned `VirtualInterface` type has no
  field for it, the mapper never copies it, and the adapter never calls
  `DescribeRouterConfiguration` (which renders the auth key into the returned
  router configuration).
- NEVER persist MACsec connectivity association key names (CKN) or secret ARNs
  on connections or LAGs. Only the boolean `MacSecCapable` capability flag is
  surfaced; `MacSecKeys` is never mapped.
- Every relationship sets a non-empty `target_type`. The Direct Connect gateway
  resource uses `resource_type = aws_direct_connect_gateway` and the bare
  gateway ID as `resource_id` so the
  `transit_gateway_attachment_to_direct_connect_gateway` edge the
  transitgateway scanner emits resolves. The gateway-to-transit-gateway edge
  targets `aws_ec2_transit_gateway`; the gateway-to-virtual-private-gateway edge
  targets `aws_vpc_vpn_gateway`. Both join on bare AWS IDs.
- NEVER hardcode `arn:aws:`. Edges key on bare AWS resource IDs, not synthesized
  ARNs.
- A gateway association whose associated gateway type is neither transit nor
  virtual private gateway emits no edge rather than a fabricated target.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from resource names or tags.

## Common Changes

- Add a new Direct Connect metadata field by extending the relevant type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Never add an auth-key- or MACsec-key-shaped
  field.
- Add new relationship evidence only when Direct Connect reports both sides
  directly, and always set a non-empty `target_type` with a join key that
  matches the target scanner resource_id.
- Extend SDK pagination and Describe fan-out in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or persist BGP auth keys or MACsec key material in any form.
- Do not call `DescribeRouterConfiguration` or any Direct Connect mutation API.
- Do not change the `aws_direct_connect_gateway` resource_type or the bare-ID
  resource_id without re-checking the transitgateway scanner's
  `transit_gateway_attachment_to_direct_connect_gateway` edge target; they must
  stay in lockstep or the edge dangles again.
- Do not add graph writes, reducer logic, or query behavior here.
- Do not add AWS credential loading or STS calls to this package.
