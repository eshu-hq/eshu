# Compute Optimizer Runtime Binding

## Purpose

`internal/collector/awscloud/services/computeoptimizer/runtimebind` self-registers
the Compute Optimizer scanner with the `awsruntime` registry through an `init`
side effect. Importing the package (directly or via
`awsruntime/bindings`) makes `service_kind "computeoptimizer"` resolvable through
`DefaultScannerFactory` with no central switch.

## Ownership boundary

This package owns only the registration wiring: it binds the
`computeoptimizer.Scanner` to the `computeoptimizer/awssdk.Client`. It owns no
fact selection, SDK behavior, or identity keying.

## Exported surface

None. The package is imported for its `init` side effect.

## How it is wired

`awsruntime.Register` is called in `init` with a `ScannerRegistration` whose
`ServiceKind` is `awscloud.ServiceComputeOptimizer` and whose `Build` constructs
the scanner with an SDK-backed client from the provided `ScannerDeps`. The one
blank-import line for this package lives in
`awsruntime/bindings/bindings.go` (append-only, alphabetical).

## Evidence

No-Regression Evidence: registration-only package; new binding, no change to existing hot paths. `go test ./internal/collector/awscloud/services/computeoptimizer/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Related docs

- `../README.md` - scanner contract.
- `../awssdk/README.md` - SDK adapter.
