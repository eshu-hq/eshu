# Application Auto Scaling Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/applicationautoscaling/bind`
registers the Application Auto Scaling scanner with the `runtime` registry
through an `init` side effect. It has no exported surface.

Importing this package (normally via
`internal/collector/cloud/aws/runtime/bindings`) installs a builder for
`service_kind` `applicationautoscaling` so `DefaultScannerFactory` can construct
the scanner with a live SDK adapter without a central switch statement.

## Ownership boundary

This package owns only the registry wiring: it connects the scanner package and
its `sdk` adapter to the runtime. It owns no fact selection, mapping, or SDK
logic.

## Evidence

No-Regression Evidence: metadata-only control-plane scanner; new read path, no change to existing hot paths. `go test ./internal/collector/cloud/aws/service/applicationautoscaling/...` green.
No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.
