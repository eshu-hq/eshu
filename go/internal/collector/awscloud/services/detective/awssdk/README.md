# AWS Detective SDK Adapter

## Purpose

`internal/collector/awscloud/services/detective/awssdk` adapts the AWS SDK for
Go v2 Amazon Detective client into the metadata-only `detective.Client`
interface. It owns Detective pagination, the safe metadata mapping, throttle
classification, and per-call AWS API telemetry.

The accepted SDK surface is limited to three read-only list operations:
`ListGraphs`, `ListMembers`, and `ListTagsForResource`. It excludes every
investigation, indicator, finding-datasource, and mutation API by construction.

## Ownership boundary

This package owns AWS SDK access for Detective. It does not own scanner-level
fact selection, Detective domain identity decisions, or fact emission. Those
belong to `internal/collector/awscloud/services/detective`.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - implements `detective.Client` over the AWS SDK Detective client.
- `NewClient` - builds the adapter for one claimed AWS boundary.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/detective` and its `types` package for
  the Detective control-plane client and response shapes.
- `internal/collector/awscloud` for the boundary and shared API-call recorder.
- `internal/collector/awscloud/services/detective` for the scanner-owned types.
- `internal/telemetry` for spans and bounded metric attributes.

## Telemetry

Each paginator page or point read is wrapped in `recordAPICall`, which starts
the shared `aws.service.pagination.page` span and increments
`eshu_dp_aws_api_calls_total` and, on throttle, `eshu_dp_aws_throttle_total`.
Metric labels stay bounded to service, account, region, operation, and result.

## Gotchas / invariants

- Keep the `apiClient` interface limited to `ListGraphs`, `ListMembers`, and
  `ListTagsForResource`. The reflection gate in `client_test.go` fails on any
  method outside that allow-set and on any forbidden substring (investigation,
  indicator, datasource, and mutation tokens) found on a non-allow-set method.
  The substring scan skips the allow-set, so the safe `ListTagsForResource`
  read is not mis-flagged by the `Tag` mutation token.
- A member's contact email (`MemberDetail.EmailAddress`) is personal data and
  is never read into the scanner-owned `MemberAccount` type. The deprecated
  usage-volume, graph-utilization, and master-id fields are dropped too.
- Map only safe identity, status, and data-source package-name metadata. From
  each member's `DatasourcePackageIngestStates`, only the package keys survive;
  the per-package ingest-state value is not carried.
- Behavior graph ARNs are passed through unchanged so synthesized identities
  inherit the graph's partition. Never reconstruct an ARN with a hardcoded
  partition.
- A graph with a blank ARN has no stable identity and is skipped.
- Pagination stops when the next token is empty or repeats the previous token,
  guarding against a same-token loop.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Related docs

- `../README.md` for the Detective scanner contract.
- `../../../README.md` for the AWS cloud envelope contract.
- `docs/public/services/collector-aws-cloud.md` for AWS collector runtime
  requirements.
