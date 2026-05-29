# AGENTS.md - internal/collector/awscloud/services/networkfirewall/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - the apiClient interface, firewall listing, tag reads, and
   telemetry plumbing.
3. `mapper.go` - firewall policy, rule group, and TLS inspection configuration
   listing and mapping.
4. `exclusion_test.go` - the reflection gate that forbids mutation and
   rule-body read methods.
5. `../scanner.go` and `../README.md` - scanner-owned fact selection.
6. `docs/public/services/collector-aws-cloud-security.md` - Network Firewall
   data boundaries.

## Invariants

- The `apiClient` interface must list read-only List/Describe operations only.
  Never add a Create/Update/Delete/Associate/Disassociate/Put/Tag/Untag method.
  Never add `DescribeRuleGroup` or `DescribeRuleGroupSummary`; the rule group
  read must stay on `DescribeRuleGroupMetadata`. The reflection test enforces
  this.
- `mapRuleGroup` persists type and capacity only; never copy a rule source
  (Suricata signature bodies) into scanner-owned types.
- `mapFirewallPolicy` persists default-action names and reference ARNs only;
  never copy the full policy rule body.
- `mapTLSInspectionConfiguration` persists response metadata only; never copy
  certificate bodies or TLS scope rule bodies.
- Wrap each AWS list page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.

## Common Changes

- Add a new Network Firewall metadata read by extending `networkfirewall.Client`,
  writing an adapter test first, then mapping the SDK response into scanner-owned
  types. Never map a field that carries a rule source, policy rule body, or
  certificate body.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not add a mutation method to `apiClient`.
- Do not switch the rule group read to `DescribeRuleGroup`; its output carries
  the rule source.
- Do not read or persist rule sources, policy rule bodies, or certificate
  bodies.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
