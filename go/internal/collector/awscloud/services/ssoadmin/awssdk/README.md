# AWS IAM Identity Center SDK Adapter

## Purpose

`internal/collector/awscloud/services/ssoadmin/awssdk` adapts AWS sso-admin and
identitystore SDK reads into the scanner-owned `ssoadmin.Snapshot`. It owns
pagination, control-plane region selection, API telemetry, and the
metadata-only API surface. It is the only place in the Identity Center scanner
path that imports the AWS SDK.

## Ownership boundary

This package owns SDK pagination and mapping for Identity Center. It does not
own scanner fact selection (that is `ssoadmin`), STS credentials, workflow
claims, fact persistence, graph writes, or reducer admission.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the `ssoadmin.Client` implementation. `Snapshot` returns the
  metadata-only Identity Center view for one claimed account.
- `NewClient` - builds the adapter for one claimed AWS boundary and forces the
  `us-east-1` control-plane region for sso-admin and identitystore.
- `SSOAdminEndpointRegion` - the org control-plane region constant.

The unexported `ssoAdminAPI` and `identityStoreAPI` interfaces are the only ways
the adapter reaches AWS. `contract_test.go` asserts their method shape, which is
the load-bearing proof for the metadata-only contract.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/ssoadmin` and `.../identitystore` for
  control-plane reads.
- `internal/collector/awscloud` for boundaries and API-call telemetry.
- `internal/collector/awscloud/services/ssoadmin` for scanner-owned types.
- `internal/telemetry` for spans and instruments.

## Telemetry

`Snapshot` wraps each AWS call in `recordAPICall`, which starts the
`aws.service.pagination.page` span and increments `eshu_dp_aws_api_calls_total`
and `eshu_dp_aws_throttle_total` with bounded service, account, region,
operation, and result attributes.

## Gotchas / invariants

- The two AWS interfaces must never gain GetInlinePolicyForPermissionSet,
  GetPermissionsBoundaryForPermissionSet, GetApplicationAccessScope,
  ListApplicationAccessScopes, ListGroupMemberships, or any mutation API. The
  reflection test fails if they do.
- Principal resolution reads only the identity store `DisplayName`. Do not map
  addresses, emails, phone numbers, birthdate, structured name, or extensions.
- Application mapping must never read access-scope attributes.
- Account assignments require an account list per permission set
  (`ListAccountsForProvisionedPermissionSet`) before
  `ListAccountAssignments`, which is account- and permission-set-scoped.
- AccessDenied/Unauthorized on the first reads produces a skip warning, not a
  hard error, so non-org-aware credentials degrade gracefully.

## Related docs

- `../README.md` - scanner contract and invariants.
- `docs/public/services/collector-aws-cloud-scanners.md`
- `docs/public/services/collector-aws-cloud-security.md`
