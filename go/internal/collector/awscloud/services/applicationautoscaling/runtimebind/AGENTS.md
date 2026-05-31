# AGENTS.md - applicationautoscaling/runtimebind guidance

## Read First

1. `README.md` - binding purpose and ownership boundary.
2. `bind.go` - the `awsruntime.Register` call.
3. `../README.md` - parent scanner contract.

## Invariants

- The package exposes no exported identifiers; it registers via `init` only.
- Keep the binding in lockstep with the scanner's `ServiceKind` constant
  (`awscloud.ServiceApplicationAutoScaling`).
- Do not add scan logic here; this package only wires the scanner and its
  `awssdk` adapter into the registry.

## Verification

```
go test ./internal/collector/awscloud/services/applicationautoscaling/runtimebind/... -count=1
```
