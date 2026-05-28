# AGENTS.md - internal/collector/awscloud/services/batch guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Batch domain types.
3. `scanner.go` - compute-environment, job-queue, job-definition,
   scheduling-policy, recent-job, and relationship emission.
4. `relationships.go` - relationship target-type and join-key construction.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - Batch slice
   requirements.

## Invariants

- Keep Batch API access behind `Client`; do not import the AWS SDK into this
  package.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from compute-environment names, job-definition
  families, images, or tags.
- Redact container environment values with `internal/redact` before they cross
  the scanner boundary. The scanner requires a redaction key.
- Never persist container command lists or job parameters. The `Container` and
  `JobDefinition` types must not declare those fields.
- Never persist scheduling-policy fair-share weight state. The
  `SchedulingPolicy` type must not declare a `FairsharePolicy` field.
- Preserve container secret `value_from` ARN references as relationship edges
  and never attempt to resolve or read secret values.
- Every relationship must set a non-empty `target_type` matching the target
  scanner's `resource_id` form. Do not emit empty target types.
- Wrap client errors with `%w`; never swallow partial failures.

## Common Changes

- Add a new Batch resource by extending the scanner-owned type, writing a
  focused scanner test first, then mapping it through `awscloud` envelope
  builders.
- Add new compute-environment or job-definition fields only when the Batch API
  reports them directly and the field is safe for persistence.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not resolve Batch jobs, definitions, or images to source repositories
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not drop the redaction requirement or add a Command/Parameters field.
