# AWS FSx SDK Adapter

## Purpose

`internal/collector/awscloud/services/fsx/awssdk` adapts AWS SDK for Go v2 FSx
describe calls into the scanner-owned metadata types defined by the parent
`fsx` package. It owns SDK pagination, SDK-to-scanner mapping, per-flavor
configuration reads, and API-call telemetry.

## Ownership boundary

This package owns the AWS SDK seam for FSx. It does not own scanner fact
selection (parent `fsx` package), STS credential loading, workflow claims, or
fact persistence. It implements `fsx.Client`.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the FSx SDK adapter. Implements `fsx.Client`.
- `NewClient` - builds a `Client` for one claimed AWS boundary from an
  `aws.Config`, boundary, tracer, and telemetry instruments.

The unexported `apiClient` interface is the single SDK seam. `Client.client` is
typed as `apiClient`, pinned by `var _ apiClient = (*awsfsx.Client)(nil)`, so it
is the only place SDK methods can be called from.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/fsx` and its `types` package for the FSx
  SDK client and response types.
- `internal/collector/awscloud` for the boundary and the shared API-call
  recorder.
- `internal/collector/awscloud/services/fsx` for the scanner-owned target types.
- `internal/telemetry` for spans, metric instruments, and attribute helpers.

## Telemetry

Each describe page is wrapped in `recordAPICall`, which starts an
`aws.service.pagination.page` span and increments `eshu_dp_aws_api_calls_total`
(and `eshu_dp_aws_throttle_total` on throttle-shaped errors). Labels are bounded
to service, account, region, operation, and result.

## Gotchas / invariants

- The `apiClient` interface is limited to the five describe reads. Any added
  method must keep `TestAPIClientInterfaceExcludesMutationAndContentAPIs` green;
  the test rejects any method whose name contains a mutation verb
  (Create/Update/Delete/Restore/Copy/Release/Associate/...).
- The mapping layer never maps Active Directory self-managed credentials, the
  ONTAP fsxadmin password, or the SVM admin password. The describe-time
  `SelfManagedActiveDirectoryAttributes` carries no password, but the adapter
  also drops `UserName`, `FileSystemAdministratorsGroup`, `DnsIps`, and the
  domain-join secret ARN. Only the AWS Managed Microsoft AD directory ID is
  read.
- Per-flavor deployment type, throughput capacity, preferred subnet, and (for
  Lustre) per-unit storage throughput are read from the
  Windows/Ontap/OpenZFS/Lustre configuration blocks; nil blocks are skipped.
- Each paginator loop guards against a nil page before iterating.

## Related docs

- `../README.md` - FSx scanner contract.
- `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector service
  coverage.
