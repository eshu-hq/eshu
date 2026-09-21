# services/vpc/runtimebind

## Purpose

Binds the production VPC scanner into the `awsruntime` registry by package
init. Importing this package for its side effect adds the scanner builder for
`awscloud.ServiceVPC` so `awsruntime.DefaultScannerFactory` can resolve a
"vpc" service claim without a central `case` block.

## Contract

- Exactly one `awsruntime.Register` call in `init()` with
  `awscloud.ServiceVPC` and a builder that constructs the scanner with the
  per-claim AWS SDK adapter.
- No AWS configuration load, network IO, or claim validation at package load
  time. The builder runs per claim with `awsruntime.ScannerDeps`.
- No cross-service imports. The package depends only on `awscloud`,
  `awsruntime`, the VPC scanner package, and the VPC SDK adapter.

## Tests

- `bind_test.go` verifies `awsruntime.LookupBuilder(awscloud.ServiceVPC)`
  returns a non-nil builder after the package is imported.

## Related docs

- `../README.md` — VPC scanner contract.
- `../../../awsruntime/README.md` — awsruntime registry semantics.
- `docs/public/guides/collector-authoring.md` — AWS scanner registration.
