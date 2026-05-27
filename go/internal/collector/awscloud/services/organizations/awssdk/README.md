# AWS Organizations SDK Adapter

## Purpose

`internal/collector/awscloud/services/organizations/awssdk` adapts the AWS SDK
for Go v2 Organizations client into the scanner-owned metadata types used by
`internal/collector/awscloud/services/organizations`.

## Ownership boundary

This package owns Organizations SDK pagination, response mapping, API-call
telemetry, and org-aware skip classification. The parent `organizations`
package owns fact-envelope construction and account redaction. Credential
loading, workflow claims, fact persistence, graph writes, reducer admission,
and query behavior live outside this package.

## Exported surface

See `doc.go` for the godoc contract.

- `OrganizationsEndpointRegion` - the commercial AWS Organizations endpoint
  region used for service clients.
- `Client` - runtime adapter implementing `organizations.Client`.
- `NewClient` - constructs a client for one claimed AWS boundary and forces the
  SDK config to `us-east-1` for Organizations API calls.

## Dependencies

- AWS SDK for Go v2 `service/organizations` for metadata-only Organizations
  reads.
- `internal/collector/awscloud` for API-call status and warning constants.
- `internal/collector/awscloud/services/organizations` for scanner-owned
  metadata types.
- `internal/telemetry` for bounded metric attributes and span names.

## Telemetry

Every Organizations API call records:

- `aws.service.pagination.page`
- `eshu_dp_aws_api_calls_total`
- `eshu_dp_aws_throttle_total` when the SDK returns a throttle-shaped error.

The parent runtime records `eshu_dp_aws_org_access_skipped_total` when this
adapter returns an org-aware skip warning. Labels stay bounded to service,
account, region, operation, result, and skip reason.

## Gotchas / invariants

- AWS Organizations uses the commercial `us-east-1` control-plane endpoint for
  this collector path. `NewClient` overwrites the SDK config region for API
  calls; the claim boundary region remains the operator-visible claim label.
- `DescribeOrganization` and `ListRoots` establish the org-aware boundary. If
  the SDK reports `AWSOrganizationsNotInUseException` or
  `AccessDeniedException`, the adapter returns a skipped snapshot with one
  warning instead of failing the claim.
- The adapter calls `ListPolicies` and `ListTargetsForPolicy`, not
  `DescribePolicy`, so SCP, RCP, tag policy, AI-services opt-out, and backup
  policy bodies never enter memory through this package.
- Disabled or unavailable policy families are skipped because `ListRoots`
  already reports policy-family enablement and absence is safe metadata. Do not
  fail an org scan just because a root has not enabled every policy family.
- Delegated administrators are expanded with
  `ListDelegatedServicesForAccount` so scanner facts include service-principal
  bindings.
- Account email and account name values are passed to the parent scanner for
  redaction. Do not log, span, metric-label, or persist them here.

## Related docs

- `../README.md`
- `docs/public/services/collector-aws-cloud-scanners.md`
- `docs/public/services/collector-aws-cloud-security.md`
