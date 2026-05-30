# AWS Amplify SDK Adapter

## Purpose

`internal/collector/awscloud/services/amplify/awssdk` adapts the AWS SDK for
Go v2 Amplify client into the metadata-only `amplify.Client` the scanner
consumes. It paginates `ListApps`, `ListBranches`, and `ListDomainAssociations`
and maps each response into scanner-owned records.

## Ownership boundary

This package owns Amplify SDK pagination, response mapping, secret-field
dropping, and per-call telemetry. It does not own scanner fact selection,
relationship rules, fact persistence, or graph writes; those belong to the
parent `amplify` package and the shared `awscloud` envelope contract.

## Exported surface

- `Client` - the metadata-only adapter implementing `amplify.Client`.
- `NewClient` - constructs the adapter for one claimed AWS boundary.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/amplify` for the SDK client and types.
- `internal/collector/awscloud` for the boundary and API-call recorder.
- `internal/collector/awscloud/services/amplify` for the scanner-owned types.
- `internal/telemetry` for spans, counters, and bounded attributes.

## Telemetry

Each paginator page is wrapped in `recordAPICall`, which opens the
`aws.service.pagination.page` span and increments the AWS API-call and throttle
counters with attributes bounded to service, account, region, operation, and
result.

## Gotchas / invariants

- Keep Amplify SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Keep `apiClient` metadata-only. Do not add any `Create*`, `Update*`,
  `Delete*`, `Start*`, `Generate*`, `Tag*`, or webhook method; the reflection
  guard test (`exclusion_test.go`) enforces this. The read surface is `List*`
  only, so an app's environment variables, build-spec secrets, basic-auth
  credentials, and repository access tokens cannot be mutated or read as a write.
- Never copy an app or branch environment-variable map, build-spec body, or
  basic-auth credential into a scanner type. The mappers drop those fields.
- Route every repository URL through `amplify.SanitizeRepositoryURL` so an
  embedded token is stripped at the boundary.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Related docs

- `../README.md` for the Amplify scanner contract.
- `../../../README.md` for the AWS cloud envelope contract.
- `docs/public/services/collector-aws-cloud.md` for AWS collector coverage.
