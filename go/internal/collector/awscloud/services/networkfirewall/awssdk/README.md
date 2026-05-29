# AWS Network Firewall SDK Adapter

## Purpose

`internal/collector/awscloud/services/networkfirewall/awssdk` adapts AWS SDK for
Go v2 Network Firewall reads into the scanner-owned types defined in the parent
package. It owns Network Firewall list/describe pagination, tag reads, and
API-call telemetry.

## Ownership boundary

This package owns Network Firewall SDK access only. It does not own
scanner-level fact selection (parent package), credential acquisition
(awsruntime), or fact persistence. It never calls a Network Firewall mutation
API and never reads rule sources, policy rule bodies, or certificate bodies.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - implements the parent `networkfirewall.Client` interface.
- `NewClient` - builds the adapter for one claimed boundary; Network Firewall is
  regional, so the adapter scans the boundary region directly.

The unexported `apiClient` interface lists the read-only Network Firewall
operations the adapter consumes. `exclusion_test.go` reflects over it to fail
the build path if any mutation or rule-body read method (including
`DescribeRuleGroup`) appears.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/networkfirewall` and its `types`
  package.
- `internal/collector/awscloud` for the boundary, API-call recorder, and
  throttle classification.
- `internal/collector/awscloud/services/networkfirewall` for scanner-owned
  types.
- `internal/telemetry` for spans and instruments.

## Telemetry

Each list page and detail/tag read is wrapped in `recordAPICall`, which emits
the `aws.service.pagination.page` span and increments
`eshu_dp_aws_api_calls_total` and `eshu_dp_aws_throttle_total`. Metric labels
stay bounded to service, account, region, operation, and result.

## Gotchas / invariants

- Rule group metadata comes from `DescribeRuleGroupMetadata`, never
  `DescribeRuleGroup`. `DescribeRuleGroupOutput.RuleGroup` carries the rule
  source (Suricata signature bodies); `DescribeRuleGroupMetadataOutput` does
  not, so the rule bodies are unreachable by construction. A test asserts the
  adapter never calls `DescribeRuleGroup`.
- `mapFirewallPolicy` reads only the policy's default-action names and the rule
  group / TLS inspection configuration reference ARNs. No rule body is read.
- `mapTLSInspectionConfiguration` reads only the response metadata; certificate
  bodies (`Certificates`, `CertificateAuthority`) and TLS scope rule bodies are
  never read.
- `ListRuleGroups` sets `Scope=ACCOUNT` so only customer-owned rule groups are
  listed; managed rule groups stay with the vendor.
- Network Firewall list APIs are not standard SDK paginators; the adapter loops
  on `NextToken` explicitly.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Related docs

- `../README.md` - Network Firewall scanner contract.
- `docs/public/services/collector-aws-cloud-security.md` - Network Firewall data
  boundaries.
