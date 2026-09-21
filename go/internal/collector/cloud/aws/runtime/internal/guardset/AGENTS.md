# AGENTS - runtime/internal/guardset guidance

## Read First

1. `README.md` - purpose, derived sources, and why the guard is not tautological.
2. `doc.go` - godoc contract for the four exported helpers.
3. `../../bindings/all.go` - the source the guard parses.
4. `../../bindings/all_test.go` and `../../registry_supported_services_test.go`
   - the two guard tests that consume these helpers.

## Invariants

- NEVER import the `runtime` registry (or anything that reads
  `SupportedServiceKinds()`) into this package. The expected scanner set MUST be
  derived from the filesystem and the `all.go` source only. Deriving it
  from the registry would make the guard tautological.
- Keep the helpers pure and table-testable. Filesystem and source reads take an
  explicit path argument so callers resolve location via `runtime.Caller`.
- Keep the `Diff` "dir present but not imported" negative test in
  `set_test.go`. It is the proof the guard still catches an unwired
  scanner.

## Common Changes

- If the bind import path shape changes, update `importPathSuffix` and the
  leaf-segment parsing in `ServiceFromImportPath` together and extend
  `TestServiceFromImportPath`.
- Adding an AWS scanner needs NO change here. The guard derives the set
  automatically from the new `service/<service>/bind/` directory and the
  one blank import appended to `all.go`.

## What Not To Change Without An ADR

- Do not turn this into runtime code. It is test-support only and must not be
  imported by any non-test package.
