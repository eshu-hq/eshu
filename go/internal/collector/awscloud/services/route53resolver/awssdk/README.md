# AWS Route 53 Resolver SDK Adapter

## Purpose

`internal/collector/awscloud/services/route53resolver/awssdk` adapts the AWS
SDK for Go v2 Route 53 Resolver client into the scanner-owned records the
route53resolver scanner consumes. It owns pagination, per-resource
count-derivation Get reads, SDK-to-scanner mapping, and AWS API telemetry for
the Route 53 Resolver read surface.

## Ownership boundary

This package owns SDK pagination, count-derivation reads, response mapping,
throttle detection, and pagination spans. It does not own fact emission,
credential acquisition, or graph projection. The scanner-owned domain types
live in the parent `route53resolver` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the SDK adapter implementing `route53resolver.Client`.
- `NewClient` - constructs the adapter for one claimed AWS boundary.

The package depends on an unexported `apiClient` interface that names only the
List and Get reads the adapter calls, so the metadata-only read surface stays
explicit and auditable.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/route53resolver` and its `types`
  package for the client, paginators, and response shapes.
- `internal/collector/awscloud` for the boundary, API-call recording, and the
  shared throttle/telemetry helpers.
- `internal/collector/awscloud/services/route53resolver` for the scanner-owned
  record types this adapter produces.
- `internal/telemetry` for the AWS API-call counters and pagination span name.

## Telemetry

The adapter records one `aws.service.pagination.page` span per API call and
increments `eshu_dp_aws_api_calls_total` (and `eshu_dp_aws_throttle_total` on
throttles) with bounded service, account, region, operation, and result
labels. It never puts ARNs, domain names, IP addresses, or tags into metric
labels.

## Gotchas / invariants

- The accepted `apiClient` interface is List and Get only. It excludes every
  Create/Update/Delete/Associate/Disassociate operation and, critically, the
  DNS Firewall domain reader (`ListFirewallDomains`) and rule reader
  (`ListFirewallRules`). `exclusion_test.go` reflects over the interface and
  fails the build if a mutation or forbidden content reader becomes reachable.
- Firewall rule group rule counts come from `GetFirewallRuleGroup` and domain
  list domain counts come from `GetFirewallDomainList`. Both Get responses
  carry only metadata plus an aggregate count; neither returns rule bodies or
  domain entries.
- `listEndpointSubnets` reads endpoint IP addresses only to collect the distinct
  subnet IDs. The IP strings are never mapped into a scanner-owned type.
- `mapResolverRule` discards `TargetIps`; only the domain name and rule type
  survive.
- `mapQueryLogConfig` carries the destination ARN only; query log records are
  read by no operation on the surface.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/route53resolver/...`
covers the bounded Route 53 Resolver metadata path: one paginated
ListResolverEndpoints stream with a per-endpoint paginated
ListResolverEndpointIpAddresses fan-out for subnet derivation, one paginated
ListResolverRules stream, one paginated ListResolverRuleAssociations stream, one
paginated ListFirewallRuleGroups stream with a per-group GetFirewallRuleGroup
count read, one paginated ListFirewallDomainLists stream with a per-list
GetFirewallDomainList count read, one paginated
ListFirewallRuleGroupAssociations stream, and one paginated
ListResolverQueryLogConfigs stream. No mutation, domain-content, or
query-log-record API is reachable, and the collector performs no graph writes.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers endpoint/rule/association/firewall/query-log fact emission, every
relationship's non-empty target type and join key, domain-list and rule-group
count-not-contents assertions, query-log destination-only assertion, runtime
registration, and command configuration. The adapter reflection contract tests
prove the mutation APIs, `ListFirewallDomains`, and `ListFirewallRules` are
unreachable.

Collector Observability Evidence: Route 53 Resolver uses the existing AWS
collector `aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`,
`eshu_dp_aws_resources_emitted_total{service="route53resolver"}`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and
resource type.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses Route 53 Resolver scans through `aws.service.scan`,
`aws.service.pagination.page`, API/throttle counters, resource/relationship
counters, and `aws_scan_status`. No new instrument or label was added.

## Related docs

- `../README.md` for the Route 53 Resolver scanner contract.
- `../../../awsruntime/README.md` for the runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
