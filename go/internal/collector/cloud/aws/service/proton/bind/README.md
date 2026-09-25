# Proton runtime binding

`package bind` self-registers the AWS Proton scanner with the
`runtime` registry. Importing it for its side effect (the blank import in
`runtime/bindings/all.go`) makes the `proton` service_kind available
through `runtime.DefaultScannerFactory`.

## Contract

- One `runtime.Register` call in `init()`, keyed by `aws.ServiceProton`.
- The builder constructs `proton.Scanner` with the `sdk` adapter from
  claim-scoped `ScannerDeps` (AWS config, boundary, tracer, instruments). It does
  no configuration loading, validation, or network IO at package load time.

## Verification

```bash
cd go
go test ./internal/collector/cloud/aws/service/proton/bind/ -count=1
```
