# Proton runtime binding

`package runtimebind` self-registers the AWS Proton scanner with the
`awsruntime` registry. Importing it for its side effect (the blank import in
`awsruntime/bindings/bindings.go`) makes the `proton` service_kind available
through `awsruntime.DefaultScannerFactory`.

## Contract

- One `awsruntime.Register` call in `init()`, keyed by `awscloud.ServiceProton`.
- The builder constructs `proton.Scanner` with the `awssdk` adapter from
  claim-scoped `ScannerDeps` (AWS config, boundary, tracer, instruments). It does
  no configuration loading, validation, or network IO at package load time.

## Verification

```bash
cd go
go test ./internal/collector/awscloud/services/proton/runtimebind/ -count=1
```
