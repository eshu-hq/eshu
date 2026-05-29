# AWS Elastic Beanstalk SDK Adapter

## Purpose

`internal/collector/awscloud/services/elasticbeanstalk/awssdk` adapts the AWS
SDK for Go v2 Elastic Beanstalk client to the Elastic Beanstalk scanner
`Client` contract. It translates SDK describe responses into scanner-owned
records and records AWS API telemetry.

## Ownership boundary

This package owns Elastic Beanstalk describe-call pagination, SDK response
mapping, AWS API telemetry, throttle detection, and pagination spans. It does
not own scanner-owned fact selection, redaction policy, or fact emission; those
belong to the parent `elasticbeanstalk` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the SDK-backed implementation of
  `elasticbeanstalk.Client`.
- `NewClient` - builds a `Client` for one claim-scoped AWS config and boundary.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk` for the SDK client and
  types.
- `internal/collector/awscloud` for API-call telemetry recording and the
  boundary type.
- `internal/collector/awscloud/services/elasticbeanstalk` for the scanner-owned
  record types.
- `internal/telemetry` for instruments, span names, and metric attributes.

## Telemetry

Every SDK call is wrapped with `recordAPICall`, which emits the
`aws.service.pagination.page` span and increments `eshu_dp_aws_api_calls_total`
(and `eshu_dp_aws_throttle_total` on throttle errors). Metric labels are bounded
to service, account, region, operation, and result.

## Gotchas / invariants

- The accepted `apiClient` interface is metadata-only by construction: it lists
  no application/environment mutation, environment rebuild/terminate, CNAME
  swap, environment-info data-plane, or configuration-validation operation. A
  reflective guard test fails if any forbidden method becomes reachable.
- `DescribeApplications` returns all applications in one call; the AWS API has
  no pagination cursor for it.
- `DescribeEnvironments` and `DescribeApplicationVersions` paginate through a
  `NextToken` cursor; the adapter loops until the token is empty.
- `DescribeEnvironmentResources` and `DescribeConfigurationSettings` are
  per-environment single-shot calls.
- The adapter preserves option-setting values verbatim so the scanner can build
  relationship joins and apply redaction. The adapter never logs or labels
  option-setting values.
- Do not read application-version source bundle object contents or
  environment-info bundles; the scanner-owned types carry no field for them.

## Related docs

- `../README.md` for the Elastic Beanstalk scanner contract.
- `docs/public/services/collector-aws-cloud.md` for AWS collector runtime and
  security requirements.
