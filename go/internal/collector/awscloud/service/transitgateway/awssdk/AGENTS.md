# AGENTS.md - services/transitgateway/awssdk guidance

## Read First

1. `README.md` - adapter contract, read-only invariants, and telemetry shape.
2. `client.go` - paginator wiring, telemetry, and the `apiClient` interface.
3. `mapper.go` - SDK type to scanner-owned record conversion.
4. `client_test.go` - the forbidden-method reflection test and mapper tests.
5. `../README.md` - Transit Gateway scanner contract and VPC pairing.
6. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- `apiClient` MUST stay read-only. Do not embed any AWS SDK paginator or
  method-bearing interface that brings in a Create/Delete/Modify/Associate/
  Disassociate/Enable/Disable/Accept/Reject/Replace/Register/Deregister method.
  `TestAPIClientNeverIncludesForbiddenMethods` will fail. The blocklist
  explicitly includes `AssociateTransitGatewayRouteTable` and
  `EnableTransitGatewayRouteTablePropagation` from issue #732.
- Adapter MUST NOT read transit gateway routes or policy rule entries. Do not
  add `SearchTransitGatewayRoutes` or `GetTransitGatewayPolicyTableEntries`;
  do not add a rules field to the mapped `PolicyTable`.
  `TestMapPolicyTableMapsIdentityOnly` will fail.
- Cross-account peer info is mapped through as reported. Do not resolve the
  remote account with STS or organizations calls.
- All paginators MUST set `MaxResults=1000` to bound per-page work.
- All API operations MUST flow through `recordAPICall` so the per-service
  counters and span attribution stay consistent.

## Common Changes

- Add a new read API only after the scanner-owned `Client` interface needs it.
  Wire one new method to its `Describe*` paginator and add a mapper test.
- Surface a new SDK field by extending the scanner-owned record in `../types.go`
  and the matching `map*` function here, with a focused test.

## What Not To Change Without An ADR

- Do not move telemetry out of `recordAPICall`.
- Do not bypass the narrow `apiClient` interface by holding `*awsec2.Client`
  directly; the production binding goes through `NewFromConfig` and is
  type-assigned to `apiClient` via the package's compile-time check.
