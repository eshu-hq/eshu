# AGENTS.md - internal/collector/awscloud/services/ssoadmin/runtimebind guidance

## Read First

1. `README.md` - binding purpose and ownership boundary.
2. `bind.go` - the single `awsruntime.Register` call.
3. `../README.md` - Identity Center scanner contract.
4. `../../../awsruntime/README.md` - registry and runtime surface.

## Invariants

- Register exactly once from `init()`. The registry panics on duplicates.
- Keep the redaction-key guard: return a typed error when
  `ScannerDeps.RedactionKey.IsZero()`.
- Do not load AWS config, acquire credentials, or build clients at init time.
  Construct clients inside the builder per claim from `ScannerDeps`.
- This binding is reached only through
  `internal/collector/awscloud/awsruntime/bindings/bindings.go`. The one blank
  import there is the only shared-file change a new scanner makes.

## Common Changes

- Almost never. This package changes only if the scanner constructor signature
  or its required dependencies change.

## What Not To Change Without An ADR

- Do not move scanner logic, SDK calls, or redaction policy into this package.
- Do not add a `case` or import to `awsruntime/registry.go`; registration is
  self-contained here.
