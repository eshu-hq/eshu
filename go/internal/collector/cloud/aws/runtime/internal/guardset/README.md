# AWS Scanner Guard Set

## Purpose

`internal/collector/cloud/aws/runtime/internal/guardset` is test-support code
that derives the expected AWS scanner set from the repository layout, so the
guard tests no longer carry a hardcoded want-list of every service.

Before this package, two guard tests each hardcoded a parallel list of every
AWS service. Every new scanner PR had to insert its entry into both lists at
the same alphabetical spot, which produced the same merge conflict on every
wave-1 scanner merge. This package removes that treadmill: the expected set is
computed from two independent sources at test time.

## What it derives

- `BindServiceDirs(serviceDir)` walks
  `service/<service>/bind/` directories and returns the sorted service
  tokens. This is the set of scanners the repo layout says SHOULD be wired.
- `BindingsImportServices(bindingsFile)` parses `all.go` with `go/parser`
  in imports-only mode and returns the sorted service tokens from the
  `service/<service>/bind` blank imports. This is the set of scanners
  that ARE wired into the aggregator.
- `ServiceFromImportPath(path)` extracts the `<service>` token from a single
  bind import path and ignores any path that is not exactly a
  `service/<service>/bind` package.
- `Diff(dirs, imports)` reports the service tokens present in `dirs` but absent
  from `imports` (missing) and present in `imports` but absent from `dirs`
  (extra).

## Why it is not tautological

The expected set comes from the filesystem and the `all.go` source. It
never comes from `runtime.SupportedServiceKinds()`. The package imports
nothing from the `runtime` registry. The guard therefore still proves the
real property: every scanner directory is wired into the aggregator, and every
imported binding actually registers.

The set-diff catches a scanner whose `bind` package was not added to
`all.go`. A separate count check
(`len(SupportedServiceKinds()) == number of bind dirs`) catches a
binding that imports but fails to register at init.

## Ownership boundary

This package owns only the guard-set derivation helpers. It does not register
scanners, configure the runtime, or know any per-service behavior.

## Telemetry

None. This is test-support code that runs only under `go test`.

## Gotchas / invariants

- Keep this package free of any `runtime` registry import. The guard's value
  depends on the expected set being derived independently of the registry it
  checks.
- `BindServiceDirs` treats a directory as a scanner only when it has a
  `bind` subdirectory, so unrelated service-package layout does not leak
  into the set.
- The `Diff` negative case ("dir present but not imported") is unit-tested in
  `set_test.go`; do not remove it. It is the proof the guard still catches
  an unwired scanner.

## Related docs

- `../../README.md` for the runtime registry and runtime surface.
- `../../bindings/README.md` for the bindings aggregator the guard parses.
