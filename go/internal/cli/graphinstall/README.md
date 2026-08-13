# Graph Install

## Purpose

`graphinstall` owns the business logic behind `eshu install nornicdb` and
`eshu graph upgrade`: resolving an install source (a local binary, a local
archive/package, a download URL, or the pinned release manifest), verifying
its checksum and reported version, and copying the resulting NornicDB binary
into Eshu's managed home with an install manifest alongside it.

## Ownership boundary

This package owns install *logic* -- what source to resolve, what to verify,
and where the managed binary and manifest live. It does not own process
wiring: reading cobra flags or mapping errors to the CLI exit-code contract.
Those stay in `go/cmd/eshu/graph_install_cmd.go`, the cobra `RunE` wrapper,
because `go/cmd/eshu` is `package main` and nothing can import it.

It also does not run a binary itself. Verifying that a candidate really is
NornicDB means invoking `<binary> version`, and that subprocess-execution
logic belongs to the local_graph process-supervision cluster in
`go/cmd/eshu` (`readLocalGraphVersion` in `local_graph_process.go`) --
`docs/internal/design/package-restructure.md` calls that cluster out as a
real bidirectional cycle that has to move as one unit or not at all, so it
stayed out of scope for this extraction. Callers thread their
`VersionReader` implementation through `Options.ReadVersion` and
`ManagedBinaryIfPresent` instead.

## Exported surface

- `Install`, `Options`, `Result` -- resolve a source, verify it, and copy the
  binary into Eshu's managed home. `Options.ReadVersion` is required; `Install`
  returns an error immediately if it is nil. `Result`'s JSON tags are a
  stable wire contract: `eshu install nornicdb` prints `Result` as-is.
- `ManagedBinaryIfPresent` -- returns the managed binary's path if Eshu has
  one installed and it still passes version verification, or an error
  satisfying `os.IsNotExist` when none is installed. `go/cmd/eshu`'s
  `local_graph_process.go` (`resolveNornicDBBinary`) is the sole external
  caller.
- `VersionReader` -- the function type callers implement to report a
  NornicDB binary's version without this package executing anything itself.

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/eshulocal` -- `ResolveHomeDir`, used to locate Eshu's managed
  home (`~/.eshu` by default) for the installed binary and manifest paths
- `internal/query` -- `GraphBackendNornicDB`, the backend name recorded in
  the install manifest
- `internal/buildinfo` -- `AppVersion`, used to resolve the pinned release
  manifest entry for the running Eshu version
- Consumed by `go/cmd/eshu`: the `install nornicdb` wrapper
  (`graph_install_cmd.go`), `graph.go`'s `eshu graph upgrade` path
  (`graphUpgradeForLayout`), and `local_graph_process.go`'s
  `resolveNornicDBBinary`

## Telemetry

None. Install runs inline with the CLI invocation and returns a JSON result;
there is no background pipeline stage to instrument.

## Gotchas / invariants

- `ManagedBinaryIfPresent`'s `os.Stat` error is returned unwrapped on
  purpose (`//nolint:wrapcheck`): `resolveNornicDBBinary` depends on
  `os.IsNotExist(err)` matching the raw `*PathError`, which stops working if
  the error gets wrapped with `%w` -- `os.IsNotExist` unwraps only `*PathError`,
  `*LinkError`, and `*SyscallError` by type switch, not through the general
  `errors.Unwrap` chain.
- `Options.ReadVersion` and `ManagedBinaryIfPresent`'s `readVersion`
  parameter must be non-nil; both fail fast with a `graphinstall:` prefixed
  error rather than reaching a nil-pointer panic deeper in the call chain.
- The embedded `nornicdb_release_manifest.json` currently ships with zero
  releases: Eshu tracks the latest NornicDB main branch rather than pinning
  releases, so `resolvePinnedReleaseSource` (used when `--from` is empty)
  always falls through to its "install with `--from`" guidance today. See
  `release_manifest_test.go`.
- An install whose source already resolves to the same version and checksum
  as the managed binary is reported `Reused: true` and left untouched,
  unless `Options.Force` is set.

## Related docs

- `docs/public/reference/graph-backend-installation.md` -- the `eshu install
  nornicdb` operator guide (source kinds, `--force`, `--sha256`)
- `docs/public/reference/environment-ingestion-queues.md` -- documents
  `ESHU_NORNICDB_INSTALL_TIMEOUT`, the one process-environment variable this
  package itself reads (a download timeout, not process wiring)
