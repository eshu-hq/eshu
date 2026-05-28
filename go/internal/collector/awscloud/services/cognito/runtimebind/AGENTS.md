# AGENTS.md - services/cognito/runtimebind guidance

## Read First

1. `README.md` - one-binding contract and ownership boundary.
2. `bind.go` - the actual registration and redaction-key guard.
3. `../README.md` - Cognito scanner contract.
4. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- Register exactly once from `init()` with `awscloud.ServiceCognito`.
- Keep the redaction-key guard: return a typed error when
  `d.RedactionKey.IsZero()`. Cognito is a redaction-required service.
- Do not load AWS configuration or build SDK clients at init time. Builders
  construct clients per claim from `ScannerDeps`.
- Do not validate or transform claims here beyond the redaction-key guard.
  Validation belongs to awsruntime and the scanner.
- Do not import anything else from `internal/collector/awscloud/services`.
  Cross-service knowledge belongs upstream.

## Common Changes

- Update the builder body only when the Cognito scanner constructor signature
  changes. Keep the change scoped to the constructor call and the guard.

## What Not To Change Without An ADR

- Do not move the `Register` call out of `init()`.
- Do not drop the redaction-key guard.
- Do not introduce side effects (network, file IO, config parsing) at package
  load time.
