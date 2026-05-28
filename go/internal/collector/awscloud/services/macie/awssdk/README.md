# AWS Macie SDK Adapter

## Purpose

`internal/collector/awscloud/services/macie/awssdk` adapts the AWS SDK for Go v2
Amazon Macie client into the metadata-only `macie.Client` interface. It owns
Macie pagination, the safe metadata mapping, throttle classification, and
per-call AWS API telemetry.

It is the highest-redaction adapter in the collector. The accepted SDK surface
excludes sensitive-data finding reads, regex-body reads, allow-list content
reads, findings filter criteria reads, and classification-job bucket-criteria
reads by construction.

## Ownership boundary

This package owns AWS SDK access for Macie. It does not own scanner-level fact
selection, Macie domain identity decisions, redaction policy, or fact emission.
Those belong to `internal/collector/awscloud/services/macie`.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - implements `macie.Client` over the AWS SDK Macie client.
- `NewClient` - builds the adapter for one claimed AWS boundary.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/macie2` and its `types` package for the
  Macie control-plane client and response shapes.
- `internal/collector/awscloud` for the boundary and shared API-call recorder.
- `internal/collector/awscloud/services/macie` for the scanner-owned types.
- `internal/telemetry` for spans and bounded metric attributes.

## Telemetry

Each paginator page or point read is wrapped in `recordAPICall`, which starts
the shared `aws.service.pagination.page` span and increments
`eshu_dp_aws_api_calls_total` and, on throttle, `eshu_dp_aws_throttle_total`.
Metric labels stay bounded to service, account, region, operation, and result.

## Gotchas / invariants

- Keep the `apiClient` interface limited to `GetMacieSession`,
  `GetAdministratorAccount`, `ListMembers`, `ListClassificationJobs`,
  `ListAllowLists`, `ListCustomDataIdentifiers`, `ListFindingsFilters`, and
  `GetFindingStatistics`. The reflection gate in `client_test.go` fails on any
  forbidden method. `ListFindingsFilters` is the only allowed name that contains
  a forbidden substring (`ListFindings`); the gate exempts exactly that pair.
- `GetFindingStatistics` is grouped by severity label only
  (`GroupBy=severity.description`). Never group by finding type, bucket name, or
  job id.
- Map only safe identity and count metadata. The job mapper reads
  `BucketDefinitions` and `BucketCriteria` only to compute counts and discards
  their contents immediately.
- Macie returns `AccessDeniedException` with a "Macie is not enabled" message
  when the account has no session. Map only that exact case to a disabled
  session; surface every other error so a genuine authorization failure is never
  reported as a clean disabled account.
- `GetAdministratorAccount` returns `ResourceNotFoundException` for a standalone
  or administrator account; map that to an empty administrator id and surface
  every other error.
- Pagination stops when the next token is empty or repeats the previous token,
  guarding against a same-token loop.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Related docs

- `../README.md` for the Macie scanner contract.
- `../../../README.md` for the AWS cloud envelope contract.
- `docs/public/services/collector-aws-cloud.md` for AWS collector runtime
  requirements.
