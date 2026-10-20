# AGENTS.md - computeoptimizer/runtimebind guidance

## Read First

1. `README.md` - registration purpose and wiring.
2. `bind.go` - the `awsruntime.Register` call.
3. `../README.md` - scanner contract.

## Invariants

- This package exists only to self-register the Compute Optimizer scanner
  through an `init` side effect. Keep it free of fact selection, SDK behavior,
  and identity keying.
- The `ServiceKind` registered must be `awscloud.ServiceComputeOptimizer`.
- The single blank-import line for this package in
  `awsruntime/bindings/bindings.go` is append-only and alphabetical. Never
  reorder, dedupe, or reformat the other lines in that file.

## What Not To Change Without An ADR

- Do not add scanner logic, SDK calls, or graph-edge logic here.
- Do not register more than one service kind from this package.
