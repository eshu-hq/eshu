# AGENTS.md - internal/collector/awscloud/services/wafv2 guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned WAFv2 domain types.
3. `scanner.go` - resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud-security.md` - WAFv2 data
   boundaries and security review requirements.

## Invariants

- Keep WAFv2 API access behind `Client`; do not import the AWS SDK into this
  package.
- Never persist IP set address lists, regex pattern bodies, or rule Statement
  bodies (And/Or/Not/ByteMatch/Sqli/Xss search strings). Persist counts and
  reference identity only.
- Managed rule set references carry vendor and name only.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from web ACL names or tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep ARNs, tags, and reference values out of metric labels.

## Common Changes

- Add a new WAFv2 metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. Never add a field that carries an address, regex body, or
  Statement body.
- Add new relationship evidence only when the WAFv2 API reports both endpoints
  directly.
- Extend SDK pagination, scope selection, and statement walking in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not persist any address, regex, or Statement payload.
- Do not call or model WAF Classic (v1) APIs; v1 is out of scope by
  construction. The scanner imports only the WAFv2 SDK.
- Do not resolve web ACL names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
