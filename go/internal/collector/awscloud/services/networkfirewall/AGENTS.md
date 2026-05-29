# AGENTS.md - internal/collector/awscloud/services/networkfirewall guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Network Firewall domain types.
3. `scanner.go` - resource emission.
4. `relationships.go` - relationship projection.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-security.md` - Network Firewall
   data boundaries and security review requirements.

## Invariants

- Keep Network Firewall API access behind `Client`; do not import the AWS SDK
  into this package.
- Never persist rule group rule sources (Suricata signature bodies), firewall
  policy rule bodies, or TLS inspection certificate bodies. Persist identity,
  type, capacity, default-action names, and reference ARNs only.
- The firewall-to-VPC and firewall-to-subnet edges target the bare VPC id and
  subnet id (`aws_ec2_vpc`, `aws_ec2_subnet`); those nodes are owned by the EC2
  scanner.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from firewall names or tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep ARNs, tags, and reference values out of metric labels.

## Common Changes

- Add a new Network Firewall metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Never add a field that carries a rule source,
  Suricata signature body, policy rule body, or certificate body.
- Add new relationship evidence only when the Network Firewall API reports both
  endpoints directly.
- Extend SDK pagination and metadata reads in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not persist any rule source, Suricata signature body, policy rule body, or
  certificate body.
- Do not switch the rule group read from `DescribeRuleGroupMetadata` to
  `DescribeRuleGroup`; the latter's output carries the rule source.
- Do not resolve firewall names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
