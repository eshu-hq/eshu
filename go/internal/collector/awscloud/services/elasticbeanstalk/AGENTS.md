# AGENTS.md - internal/collector/awscloud/services/elasticbeanstalk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Elastic Beanstalk domain types.
3. `scanner.go` - application, environment, application-version, and
   relationship emission plus option-setting redaction.
4. `relationships.go` - environment relationship derivation and join keys.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - Elastic Beanstalk slice
   requirements.

## Invariants

- Keep Elastic Beanstalk API access behind `Client`; do not import the AWS SDK
  into this package.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from application names, environment names, or tags.
- Redact every environment option-setting value with `internal/redact` before
  it crosses the scanner boundary. Keep option names and namespaces; never keep
  values.
- Never read or persist application-version source bundle object contents or
  environment-info bundles.
- Never fabricate an `arn:aws:` string. Set a target ARN only when AWS reported
  a full ARN; keep bare names as the target id otherwise.
- Keep option-setting values, resource ARNs, and tags out of metric labels.

## Common Changes

- Add a new Elastic Beanstalk resource by extending the scanner-owned type,
  writing a focused scanner test first, then mapping it through `awscloud`
  envelope builders.
- Add a new relationship only when the Elastic Beanstalk API reports the target
  identity directly; key the join on the grepped target resource_type constant.
- Add new fields only when the API reports them directly and the field is safe
  for persistence.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not resolve Elastic Beanstalk environments to source repositories here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
