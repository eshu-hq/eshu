# AGENTS.md - computeoptimizer/bind guidance

## Read First

1. `README.md` - registration purpose and wiring.
2. `register.go` - the `runtime.Register` call.
3. `../README.md` - scanner contract.

## Invariants

- This package exists only to self-register the Compute Optimizer scanner
  through an `init` side effect. Keep it free of fact selection, SDK behavior,
  and identity keying.
- The `ServiceKind` registered must be `aws.ServiceComputeOptimizer`.
- The single blank-import line for this package in
  `runtime/bindings/all.go` is append-only and alphabetical. Never
  reorder, dedupe, or reformat the other lines in that file.

## What Not To Change Without An ADR

- Do not add scanner logic, SDK calls, or graph-edge logic here.
- Do not register more than one service kind from this package.
