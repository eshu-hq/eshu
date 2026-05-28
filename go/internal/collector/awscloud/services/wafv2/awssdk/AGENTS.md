# AGENTS.md - internal/collector/awscloud/services/wafv2/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - WAFv2 web ACL listing, scope selection, tags, protected
   resources, and telemetry.
3. `mapping.go` - rule group, IP set, and regex set listing and mapping.
4. `references.go` - the reference-only rule walk.
5. `exclusion_test.go` - the reflection gate that forbids mutation methods.
6. `../scanner.go` and `../README.md` - scanner-owned fact selection.
7. `docs/public/services/collector-aws-cloud-security.md` - WAFv2 data
   boundaries.

## Invariants

- The `apiClient` interface must list read-only List/Get operations only. Never
  add a Create/Update/Delete/Associate/Disassociate/Put/Tag/Untag/API-key/
  GetSampledRequests method. The reflection test enforces this.
- `mapIPSet` and `mapRegexPatternSet` persist counts only; never copy the
  address list or regex bodies into scanner-owned types.
- `walkRuleReferences` reads only reference statements and managed rule group
  vendor/name. Never read a match-statement body (ByteMatch, Regex, Sqli, Xss,
  Size, Geo, LabelMatch, Asn) or any search string.
- Wrap each AWS list page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Select scope from the boundary region; rebind CLOUDFRONT scope to the
  us-east-1 endpoint.

## Common Changes

- Add a new WAFv2 metadata read by extending `wafv2.Client`, writing an adapter
  test first, then mapping the SDK response into scanner-owned types. Never map
  a field that carries an address, regex body, or Statement body.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not add a mutation or data-plane method to `apiClient`.
- Do not read or persist IP set addresses, regex bodies, or rule Statement
  bodies.
- Do not call or model WAF Classic (v1) APIs.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
