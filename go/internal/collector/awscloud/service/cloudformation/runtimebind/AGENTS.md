# AGENTS.md - services/cloudformation/runtimebind guidance

## Read First

1. `README.md` - one-binding contract and ownership boundary.
2. `bind.go` - the actual registration and redaction-key guard.
3. `../README.md` - CloudFormation scanner contract.
4. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- Register exactly once from `init()` with `awscloud.ServiceCloudFormation`.
- Keep the redaction-key guard: return a typed error naming "redaction key" when
  `d.RedactionKey.IsZero()`. The CloudFormation scanner cannot redact stack
  outputs without it.
- Keep `RequiresRedactionKey: true` in the registration. The command derives the
  `ESHU_AWS_REDACTION_KEY` requirement from this flag, so dropping it would let a
  CloudFormation-only target start without a key.
- Do not load AWS configuration or build SDK clients at init time. Builders
  construct clients per claim from `ScannerDeps`.
- Do not validate or transform claims here beyond the redaction-key guard.
  Validation belongs to awsruntime and the scanner.
- Do not import anything else from `internal/collector/awscloud/services`.
  Cross-service knowledge belongs upstream.

## Common Changes

- Update the builder body only when the CloudFormation scanner constructor
  signature changes. Keep the change scoped to the constructor call and the
  redaction-key guard.

## What Not To Change Without An ADR

- Do not move the `Register` call out of `init()`.
- Do not drop the redaction-key guard.
- Do not introduce side effects (network, file IO, config parsing) at package
  load time.
