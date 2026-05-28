# AWS WAFv2 SDK Adapter

## Purpose

`internal/collector/awscloud/services/wafv2/awssdk` adapts AWS SDK for Go v2
WAFv2 reads into the scanner-owned types defined in the parent package. It owns
WAFv2 list/get pagination, scope selection, tag reads, regional protected
resource resolution, the rule-reference walk, and API-call telemetry.

## Ownership boundary

This package owns WAFv2 SDK access only. It does not own scanner-level fact
selection (parent package), credential acquisition (awsruntime), or fact
persistence. It never calls a WAFv2 mutation API and never reads sensitive
bodies.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - implements the parent `wafv2.Client` interface.
- `NewClient` - builds the adapter for one claimed boundary, selecting the
  CLOUDFRONT scope (rebound to the us-east-1 endpoint) for a global boundary
  region and REGIONAL otherwise.

The unexported `apiClient` interface lists the read-only WAFv2 operations the
adapter consumes. `exclusion_test.go` reflects over it to fail the build path
if any mutation or data-plane method appears.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/wafv2` and its `types` package.
- `internal/collector/awscloud` for the boundary, API-call recorder, and
  throttle classification.
- `internal/collector/awscloud/services/wafv2` for scanner-owned types.
- `internal/telemetry` for spans and instruments.

## Telemetry

Each list page and detail/tag read is wrapped in `recordAPICall`, which emits
the `aws.service.pagination.page` span and increments
`eshu_dp_aws_api_calls_total` and `eshu_dp_aws_throttle_total`. Metric labels
stay bounded to service, account, region, operation, and result.

## Gotchas / invariants

- `mapIPSet` counts `Addresses` and discards the slice. `mapRegexPatternSet`
  counts `RegularExpressionList` and discards it. Neither value reaches
  scanner-owned state.
- `walkRuleReferences` reads only reference-bearing fields
  (RuleGroup/IPSet/RegexPatternSet reference statements, ManagedRuleGroup
  vendor/name) and recurses into And/Or/Not and scope-down statements. Every
  match-statement body is left unread.
- The CLOUDFRONT scope has no `ListResourcesForWebACL` surface, so protected
  resources resolve for the REGIONAL scope only; CloudFront associations are
  recorded by the CloudFront scanner.
- WAFv2 list APIs are not standard SDK paginators; the adapter loops on
  `NextMarker` explicitly.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Related docs

- `../README.md` - WAFv2 scanner contract.
- `docs/public/services/collector-aws-cloud-security.md` - WAFv2 data
  boundaries.
