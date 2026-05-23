# internal/buildinfo

## Read First

1. `go/internal/buildinfo/README.md`
2. `go/internal/buildinfo/doc.go`
3. `go/internal/buildinfo/buildinfo.go`
4. `go/internal/buildinfo/cli.go`
5. `Dockerfile`

## Package Rules

- `Version` MUST only be set by release or installer `-ldflags`. Runtime code
  must use `AppVersion()` and must not assign to `Version`.
- `AppVersion()` MUST preserve the precedence order: non-`dev` linker value,
  non-`(devel)` Go main-module version, then `dev`.
- Service binaries MUST call `PrintVersionFlag` before telemetry, Postgres,
  graph, config loading, or other runtime setup.
- Do not add duplicate version constants in other packages. Additional build
  attributes need separate linker variables and accessors.
- If the ldflags import path changes, update Dockerfile/release wiring and the
  tests that assert version output.

## Proof

- Run `cd go && go test ./internal/buildinfo -count=1` for package changes.
- Smoke-test new service binaries with both `--version` and `-v`.
- Run `go run ./cmd/eshu docs verify ../go/internal/buildinfo --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
