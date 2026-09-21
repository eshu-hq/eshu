# AGENTS.md - services/sagemaker/runtimebind guidance

## Read First

1. `README.md` - one-binding contract and ownership boundary.
2. `bind.go` - the actual registration.
3. `../README.md` - SageMaker scanner contract.
4. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- Register exactly once from `init()` with `awscloud.ServiceSageMaker`.
- Keep the builder body a plain constructor call. SageMaker takes no optional
  dependency (no redaction key, no checkpoint store).
- Do not load AWS configuration or build SDK clients at init time. Builders
  construct clients per claim from `ScannerDeps`.
- Do not validate or transform claims here. Validation belongs to awsruntime
  and the scanner.
- Do not import anything else from `internal/collector/awscloud/services`.
  Cross-service knowledge belongs upstream.

## Common Changes

- Update the builder body only when the SageMaker scanner constructor signature
  changes. Keep the change scoped to the constructor call.

## What Not To Change Without An ADR

- Do not move the `Register` call out of `init()`.
- Do not introduce side effects (network, file IO, config parsing) at package
  load time.
