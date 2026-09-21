# AGENTS.md - services/acmpca/runtimebind guidance

## Read First

1. `README.md` - one-binding contract and ownership boundary.
2. `bind.go` - the actual registration.
3. `../README.md` - ACM Private CA scanner contract.
4. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- Register exactly once from `init()` with `awscloud.ServiceACMPCA`.
- Leave `RequiresRedactionKey` unset. The acm-pca scanner is metadata-only and
  routes no operator free-text through redact. Set the flag only if that
  changes, and keep the builder's `d.RedactionKey` guard in lockstep.
- Do not load AWS configuration or build SDK clients at init time. Builders
  construct clients per claim from `ScannerDeps`.
- Do not validate or transform claims here. Validation belongs to awsruntime and
  the scanner. The builder body stays a constructor call.
- Do not import anything else from `internal/collector/awscloud/services`.
  Cross-service knowledge belongs upstream.

## Common Changes

- Update the builder body only when the acm-pca scanner constructor signature
  changes. Keep the change scoped to the constructor call.

## What Not To Change Without An ADR

- Do not move the `Register` call out of `init()`.
- Do not introduce side effects (network, file IO, config parsing) at package
  load time.
