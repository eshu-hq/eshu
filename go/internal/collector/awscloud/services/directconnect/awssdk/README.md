# AWS Direct Connect SDK adapter

## Purpose

`internal/collector/awscloud/services/directconnect/awssdk` adapts AWS SDK for
Go v2 Direct Connect read responses into the scanner-owned Direct Connect
records. It owns NextToken pagination wiring, API-call telemetry, and the narrow
`apiClient` interface that the production AWS SDK Client satisfies.

## Read-only contract

The package's `apiClient` interface names only the read operations the scanner
needs:

- `DescribeConnections`
- `DescribeVirtualInterfaces`
- `DescribeDirectConnectGateways`
- `DescribeLags`
- `DescribeDirectConnectGatewayAssociations`

No mutation method (Create/Delete/Update/Associate/Disassociate/Confirm/
Allocate/Accept/Tag/Untag/Start/Stop) is named in the interface.
`TestAPIClientNeverIncludesForbiddenMethods` reflects the interface against the
Direct Connect mutation blocklist and fails if any forbidden name appears.
`TestAPIClientOnlyReadsDescribes` additionally fails if the interface ever grows
a method that is not a `Describe*` read. `DescribeRouterConfiguration` is on the
blocklist because the rendered router configuration contains the BGP
authentication key.

## Secret exclusion

- `TestMapVirtualInterfaceDropsAuthKey` feeds an `authKey` into the mapper and
  proves the scanner-owned `VirtualInterface` has no field that could carry it.
- `TestMapConnectionDropsMacSecKeys` feeds a `MacSecKey` (CKN + secret ARN) and
  proves the scanner-owned `Connection` carries only the boolean
  `MacSecCapable` capability flag, never the key material.

## Telemetry

Every `recordAPICall` wraps a single AWS call with:

- `aws.service.pagination.page` span attribution
  (`service`, `account_id`, `region`, `operation`).
- `eshu_dp_aws_api_calls_total{service="directconnect",result=...}` counter.
- `eshu_dp_aws_throttle_total{service="directconnect"}` counter for throttling
  errors.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/directconnect` — the AWS Direct Connect
  SDK.
- `internal/collector/awscloud/services/directconnect` — the scanner-owned
  record types this adapter maps into.

## Gotchas / invariants

- Adapter MUST stay read-only. Adding any mutation method fails
  `TestAPIClientNeverIncludesForbiddenMethods`.
- Adapter MUST NOT call `DescribeRouterConfiguration`; it renders the BGP auth
  key.
- The mapper MUST NOT copy `VirtualInterface.AuthKey`, BGP-peer auth keys, or
  `Connection`/`Lag` `MacSecKeys`. The scanner-owned types have no field for
  them, so the exclusion is enforced at compile time and pinned by tests.
- Direct Connect ships no generated paginators. Each list follows `NextToken`
  with `MaxResults=100`; `nextToken` treats an empty string as no more pages so
  the loop cannot spin forever.
- All API operations MUST flow through `recordAPICall` so the per-service
  counters and span attribution stay consistent.

## Related docs

- `../README.md` — Direct Connect scanner contract and join keys.
- `../../../awsruntime/README.md` — awsruntime registry and runtime surface.
- `docs/public/guides/collector-authoring.md` — AWS scanner authoring.
