# AGENTS.md - internal/collector/awscloud/services/ecs/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - ECS SDK pagination, batched describes, mapping, and telemetry.
3. `../scanner.go` - scanner-owned ECS fact selection and redaction.
4. `../README.md` - ECS scanner contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   runtime and security requirements.

## Invariants

- Keep ECS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Preserve AWS API telemetry for every SDK call, including batched describe
  calls.
- Batch ECS describe APIs at documented limits: clusters `100`, services `10`,
  and tasks `100`.
- Preserve ElasticNetworkInterface attachment details from `DescribeTasks`.
- Do not log or label task-definition environment values, secret references,
  resource ARNs, tags, or image refs.
- Do not read secret values. ECS `Secret.ValueFrom` is a reference and should
  be mapped as-is.

## Common Changes

- Add a new ECS API read by extending `ecs.Client`, writing adapter mapping
  tests, and wrapping the SDK call with `recordAPICall`.
- Add mapping fields only after confirming they are reported by ECS and safe for
  persistence.
- Keep retry and throttling behavior in the AWS SDK and telemetry wrapper; do
  not add local retry loops here without an ADR.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from ECS
  names or tags here.
- Do not add graph writes, reducer logic, or query behavior.
- Do not cache cross-account credentials or create SDK clients outside the
  claim-scoped factory path.
