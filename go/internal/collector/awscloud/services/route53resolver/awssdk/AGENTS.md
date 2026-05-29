# AGENTS.md - services/route53resolver/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - pagination, count-derivation Get reads, and telemetry wiring.
3. `mapper.go` - SDK-to-scanner record mapping.
4. `exclusion_test.go` - the metadata-only read-surface guard.
5. `../README.md` - Route 53 Resolver scanner contract.

## Invariants

- Keep the `apiClient` interface List/Get only. Never add a Create, Update,
  Delete, Associate, or Disassociate operation. `exclusion_test.go` enforces
  this; do not weaken it.
- Never add `ListFirewallDomains` (domain list contents) or `ListFirewallRules`
  (rule bodies) to `apiClient`. The exclusion test bans both by name. Counts
  come from `GetFirewallRuleGroup` and `GetFirewallDomainList`, which return
  only metadata plus an aggregate count.
- Never carry resolver endpoint IP address strings out of `listEndpointSubnets`;
  collect distinct subnet IDs only.
- Never carry `TargetIps` out of `mapResolverRule`.
- Query log configurations carry the destination ARN only; never add a query
  log record reader.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters. Keep ARNs, domain names, IP addresses, and tags
  out of metric labels.

## Common Changes

- Add a new Route 53 Resolver read by extending `apiClient`, paginating in
  `client.go`, and mapping the response in `mapper.go`. Add the field to the
  scanner-owned type in the parent package first.
- Extend pagination only with a performance note in `README.md`.

## What Not To Change Without An ADR

- Do not add a mutation, domain-content, rule-body, or query-log-record API to
  `apiClient`.
- Do not add credential loading or STS calls here.
