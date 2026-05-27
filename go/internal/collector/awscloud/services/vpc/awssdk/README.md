# AWS VPC SDK adapter

## Purpose

`internal/collector/awscloud/services/vpc/awssdk` adapts AWS SDK for Go v2 EC2
read responses into the scanner-owned VPC topology records. It owns paginator
wiring, API-call telemetry, and the narrow `apiClient` interface that the
production AWS SDK Client satisfies.

## Read-only contract

The package's `apiClient` interface embeds only the AWS SDK paginator API
interfaces for the read operations the scanner needs:

- `DescribeRouteTablesAPIClient`
- `DescribeInternetGatewaysAPIClient`
- `DescribeNatGatewaysAPIClient`
- `DescribeNetworkAclsAPIClient`
- `DescribeVpcPeeringConnectionsAPIClient`
- `DescribeVpcEndpointsAPIClient`
- `DescribeDhcpOptionsAPIClient`

and four explicit non-paginated read method signatures
(`DescribeAddresses`, `DescribeCustomerGateways`, `DescribeVpnGateways`,
`DescribeVpnConnections`) because the AWS SDK does not generate paginators for
those.

No mutation method (Create/Delete/Modify/Associate/Disassociate/Authorize/
Revoke/Allocate/Release/Replace/Accept/Reject/Attach/Detach) is embedded into
the interface. `TestAPIClientNeverIncludesForbiddenMethods` reflects the
interface against the issue #731 blocklist and fails if any forbidden name
appears. `TestAPIClientOnlyReadsListsAndDescribes` additionally fails if the
interface ever grows a method that is not a `Describe*`, `Get*`, or `List*`
read.

## Telemetry

Every `recordAPICall` wraps a single AWS call with:

- `aws.service.pagination.page` span attribution
  (`service`, `account_id`, `region`, `operation`).
- `eshu_dp_aws_api_calls_total{service="vpc",result=...}` counter.
- `eshu_dp_aws_throttle_total{service="vpc"}` counter for throttling errors.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/ec2` — the AWS EC2 SDK serves the
  VPC topology APIs.
- `internal/collector/awscloud/services/vpc` — the scanner-owned record
  types this adapter maps into.

## Gotchas / invariants

- Adapter MUST stay read-only. Adding any mutation method fails
  `TestAPIClientNeverIncludesForbiddenMethods`.
- Adapter MUST NOT persist `CustomerGatewayConfiguration` (the AWS XML body
  on `VpnConnection`) because it can carry tunnel pre-shared keys. The mapped
  `VPNConnection` struct intentionally omits that field and
  `TestMapVPNConnectionDoesNotExposeTunnelPSK` proves it.
- Paginators are constructed with `MaxResults=1000` to bound page count.
- Non-paginated APIs (`DescribeAddresses`, `DescribeCustomerGateways`,
  `DescribeVpnGateways`, `DescribeVpnConnections`) return the full set in one
  call by AWS contract; the adapter calls them once and maps the slice.

## Related docs

- `../README.md` — VPC scanner contract and EC2/VPC ownership table.
- `../../../awsruntime/README.md` — awsruntime registry and runtime surface.
- `docs/public/guides/collector-authoring.md` — AWS scanner authoring.
