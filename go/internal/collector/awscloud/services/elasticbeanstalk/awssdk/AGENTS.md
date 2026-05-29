# AGENTS.md - internal/collector/awscloud/services/elasticbeanstalk/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - Elastic Beanstalk SDK pagination, mapping, and telemetry.
3. `../scanner.go` - scanner-owned fact selection and redaction.
4. `../README.md` - Elastic Beanstalk scanner contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime and
   security requirements.

## Invariants

- Keep the accepted `apiClient` interface metadata-only. Adding any mutation,
  environment rebuild/terminate, CNAME swap, environment-info data-plane, or
  configuration-validation method breaks the reflective guard test and must not
  ship.
- Keep Elastic Beanstalk SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Preserve AWS API telemetry for every SDK call via `recordAPICall`.
- Loop `DescribeEnvironments` and `DescribeApplicationVersions` until the
  `NextToken` cursor is empty.
- Do not log or label option-setting values, resource ARNs, or tags.
- Do not read application-version source bundle object contents or
  environment-info bundles.

## Common Changes

- Add a new Elastic Beanstalk read by extending `elasticbeanstalk.Client`,
  writing adapter mapping tests, and wrapping the SDK call with `recordAPICall`.
- Add mapping fields only after confirming they are reported by Elastic
  Beanstalk and safe for persistence.
- Keep retry and throttling behavior in the AWS SDK and telemetry wrapper; do
  not add local retry loops here without an ADR.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from
  Elastic Beanstalk names or tags here.
- Do not add graph writes, reducer logic, or query behavior.
- Do not cache cross-account credentials or create SDK clients outside the
  claim-scoped factory path.
