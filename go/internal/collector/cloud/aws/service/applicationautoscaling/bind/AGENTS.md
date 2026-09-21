# AGENTS.md - applicationautoscaling/bind guidance

## Read First

1. `README.md` - binding purpose and ownership boundary.
2. `bind.go` - the `runtime.Register` call.
3. `../README.md` - parent scanner contract.

## Invariants

- The package exposes no exported identifiers; it registers via `init` only.
- Keep the binding in lockstep with the scanner's `ServiceKind` constant
  (`aws.ServiceApplicationAutoScaling`).
- Do not add scan logic here; this package only wires the scanner and its
  `sdk` adapter into the registry.

## Verification

```
go test ./internal/collector/cloud/aws/service/applicationautoscaling/bind/... -count=1
```
