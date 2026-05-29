# AWS Directory Service SDK Adapter

## Purpose

`internal/collector/awscloud/services/ds/awssdk` adapts AWS SDK for Go v2
Directory Service describe calls into the scanner-owned metadata types defined by
the parent `ds` package. It owns SDK pagination, SDK-to-scanner mapping,
per-directory LDAPS resolution, and API-call telemetry.

## Ownership boundary

This package owns the AWS SDK seam for Directory Service. It does not own scanner
fact selection (parent `ds` package), STS credential loading, workflow claims, or
fact persistence. It implements `ds.Client`.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the Directory Service SDK adapter. Implements `ds.Client`.
- `NewClient` - builds a `Client` for one claimed AWS boundary from an
  `aws.Config`, boundary, tracer, and telemetry instruments.

The unexported `apiClient` interface is the single SDK seam. `Client.client` is
typed as `apiClient`, pinned by `var _ apiClient = (*awsds.Client)(nil)`, so it
is the only place SDK methods can be called from.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/directoryservice` and its `types` package
  for the Directory Service SDK client and response types.
- `internal/collector/awscloud` for the boundary and the shared API-call
  recorder.
- `internal/collector/awscloud/services/ds` for the scanner-owned target types.
- `internal/telemetry` for spans, metric instruments, and attribute helpers.

## Telemetry

Each describe page is wrapped in `recordAPICall`, which starts an
`aws.service.pagination.page` span and increments `eshu_dp_aws_api_calls_total`
(and `eshu_dp_aws_throttle_total` on throttle-shaped errors). Labels are bounded
to service, account, region, operation, and result.

## Gotchas / invariants

- The `apiClient` interface is limited to the five describe/list reads. Any added
  method must keep `TestAPIClientInterfaceExcludesMutationAndSecretAPIs` green;
  the test requires every method to be a read-class (Describe/List/Get) call and
  rejects any method whose name contains a mutation or password verb
  (Create/Update/Delete/Enable/Disable/Reset/Password/...).
- The mapping layer never maps the directory admin password, the RADIUS shared
  secret, or the AD Connector `CustomerUserName`. `DescribeDirectories` does not
  return the admin password; the adapter additionally never reads
  `RadiusSettings`.
- VPC placement is read from `VpcSettings` for Simple AD and Managed Microsoft AD
  and from `ConnectSettings` for AD Connector; nil blocks are skipped.
- LDAPS is queried only for Managed Microsoft AD (and the shared variant). Simple
  AD and AD Connector do not support LDAPS, so the adapter skips
  `DescribeLDAPSSettings` for them to avoid an `UnsupportedOperationException`.
- Each paginator loop guards against a nil page before iterating and follows the
  `NextToken` cursor to completion.

## Related docs

- `../README.md` - Directory Service scanner contract.
- `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector service
  coverage.
