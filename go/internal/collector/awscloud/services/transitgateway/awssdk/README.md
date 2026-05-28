# AWS Transit Gateway SDK adapter

## Purpose

`internal/collector/awscloud/services/transitgateway/awssdk` adapts AWS SDK for
Go v2 EC2 read responses into the scanner-owned Transit Gateway records. It owns
paginator wiring, API-call telemetry, and the narrow `apiClient` interface that
the production AWS SDK Client satisfies.

## Read-only contract

The package's `apiClient` interface embeds only the AWS SDK paginator API
interfaces for the read operations the scanner needs:

- `DescribeTransitGatewaysAPIClient`
- `DescribeTransitGatewayRouteTablesAPIClient`
- `DescribeTransitGatewayAttachmentsAPIClient`
- `DescribeTransitGatewayPeeringAttachmentsAPIClient`
- `DescribeTransitGatewayMulticastDomainsAPIClient`
- `DescribeTransitGatewayPolicyTablesAPIClient`

No mutation method (Create/Delete/Modify/Associate/Disassociate/Enable/Disable/
Accept/Reject/Replace/Register/Deregister) is embedded into the interface.
`TestAPIClientNeverIncludesForbiddenMethods` reflects the interface against the
issue #732 blocklist (including `AssociateTransitGatewayRouteTable` and
`EnableTransitGatewayRouteTablePropagation`) and fails if any forbidden name
appears. `TestAPIClientOnlyReadsDescribes` additionally fails if the interface
ever grows a method that is not a `Describe*` read. The blocklist also covers
the route- and policy-rule readers (`SearchTransitGatewayRoutes`,
`GetTransitGatewayPolicyTableEntries`) that would expose network policy detail
beyond inventory metadata.

## Telemetry

Every `recordAPICall` wraps a single AWS call with:

- `aws.service.pagination.page` span attribution
  (`service`, `account_id`, `region`, `operation`).
- `eshu_dp_aws_api_calls_total{service="transitgateway",result=...}` counter.
- `eshu_dp_aws_throttle_total{service="transitgateway"}` counter for throttling
  errors.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/ec2` — the AWS EC2 SDK serves the
  Transit Gateway APIs.
- `internal/collector/awscloud/services/transitgateway` — the scanner-owned
  record types this adapter maps into.

## Gotchas / invariants

- Adapter MUST stay read-only. Adding any mutation method fails
  `TestAPIClientNeverIncludesForbiddenMethods`.
- Adapter MUST NOT read transit gateway routes or policy rule entries. The
  mapped `PolicyTable` has no rules field and `TestMapPolicyTableMapsIdentityOnly`
  proves it.
- Cross-account peering attachment info (`owner_id`, `region`, peer transit
  gateway ID) is mapped through as AWS reports it. The adapter does not call
  any STS or organizations API to resolve the remote account.
- Paginators are constructed with `MaxResults=1000` to bound page count.
- All API operations MUST flow through `recordAPICall` so the per-service
  counters and span attribution stay consistent.

## Related docs

- `../README.md` — Transit Gateway scanner contract and VPC pairing.
- `../../../awsruntime/README.md` — awsruntime registry and runtime surface.
- `docs/public/guides/collector-authoring.md` — AWS scanner authoring.
