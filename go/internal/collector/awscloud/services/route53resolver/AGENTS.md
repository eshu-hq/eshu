# AGENTS.md - internal/collector/awscloud/services/route53resolver guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Route 53 Resolver domain types and the
   metadata-only `Client` read surface.
3. `scanner.go` - endpoint, rule, association, firewall, and query-log fact
   emission.
4. `observations.go` - resource attribute construction.
5. `relationships.go` - relationship target-type and join-key construction.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - Route 53 Resolver
   slice requirements.

## Invariants

- Keep Route 53 Resolver API access behind `Client`; do not import the AWS SDK
  into this package.
- This is distinct from the `route53` scanner (hosted zones, DNS records). Do
  not merge the two surfaces.
- Never read or persist DNS Firewall domain list contents. Only the
  AWS-reported `domain_count` survives. The `FirewallDomainList` type must not
  declare a domains field, and the `Client` must not expose a domain reader.
- Never read or persist DNS Firewall rule bodies. Only the AWS-reported
  `rule_count` survives. The `Client` must not expose a rule reader.
- Never persist resolver endpoint IP address strings. Derive subnet
  relationships from IP addresses and keep only `ip_address_count`.
- Resolver rules carry name, domain name, and rule type only. Never persist
  forwarded target query data (`TargetIps`).
- Query log configurations carry the destination ARN only. Never read query log
  records.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from endpoint names, rule domains, or tags.
- Every relationship must set a non-empty `target_type` matching the target
  scanner's `resource_id` form. VPC and subnet edges target `aws_ec2_vpc` and
  `aws_ec2_subnet` by bare AWS-reported identifier.
- Wrap client errors with `%w`; never swallow partial failures.

## Common Changes

- Add a new Route 53 Resolver resource by extending the scanner-owned type,
  writing a focused scanner test first, then mapping it through `awscloud`
  envelope builders.
- Add new endpoint or rule fields only when the AWS API reports them directly
  and the field is safe for persistence (never domains, rule bodies, IP
  strings, forwarded targets, or query log records).
- Extend SDK pagination and count derivation in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not add a domain reader, rule reader, or query-log-record reader to the
  `Client` interface.
- Do not resolve resolver rules, endpoints, or destinations to source
  repositories here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
