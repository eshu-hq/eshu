# AGENTS.md - services/directconnect/awssdk guidance

## Read First

1. `README.md` - adapter contract, read-only invariants, and telemetry shape.
2. `client.go` - NextToken pagination wiring, telemetry, and the `apiClient`
   interface.
3. `mapper.go` - SDK type to scanner-owned record conversion.
4. `client_test.go` - the forbidden-method reflection test, the authKey-drop
   test, and the MACsec-key-drop test.
5. `../README.md` - Direct Connect scanner contract and join keys.
6. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- `apiClient` MUST stay read-only. Do not name any method that brings in a
  Create/Delete/Update/Associate/Disassociate/Confirm/Allocate/Accept/Tag/
  Untag/Start/Stop operation. `TestAPIClientNeverIncludesForbiddenMethods` will
  fail.
- Do NOT add `DescribeRouterConfiguration`. It renders the BGP authentication
  key into the returned configuration; it is on the forbidden blocklist.
- The mapper MUST NOT copy `VirtualInterface.AuthKey`, any `BgpPeers[].AuthKey`,
  or `Connection`/`Lag` `MacSecKeys`. The scanner-owned types have no field for
  these and `TestMapVirtualInterfaceDropsAuthKey` /
  `TestMapConnectionDropsMacSecKeys` prove it.
- Each list MUST follow `NextToken` with `MaxResults=100`. Use the `nextToken`
  helper so an empty-string token ends the loop.
- All API operations MUST flow through `recordAPICall` so the per-service
  counters and span attribution stay consistent.

## Common Changes

- Add a new read API only after the scanner-owned `Client` interface needs it.
  Name one new `Describe*` method on `apiClient`, wire its NextToken loop, and
  add a mapper test.
- Surface a new SDK field by extending the scanner-owned record in `../types.go`
  and the matching `map*` function here, with a focused test. Never add an
  auth-key- or MACsec-key-shaped field.

## What Not To Change Without An ADR

- Do not move telemetry out of `recordAPICall`.
- Do not call `DescribeRouterConfiguration` or any Direct Connect mutation API.
- Do not map BGP auth keys or MACsec key material in any form.
- Do not bypass the narrow `apiClient` interface by holding `*awsdx.Client`
  directly; the production binding goes through `NewFromConfig` and is
  type-assigned to `apiClient` via the package's compile-time check.
