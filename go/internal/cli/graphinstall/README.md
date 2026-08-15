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

It does not run the candidate NornicDB binary. (It does run one other
subprocess: `pkgutil --expand-full`, to expand a darwin `.pkg` install
source -- archive extraction, not version verification.) Verifying that a
candidate really is NornicDB means invoking `<binary> version`, and that
subprocess-execution logic belongs to the local_graph process-supervision
cluster in
`go/internal/cli/localsupervisor` (`readLocalGraphVersion` in
`graph_process.go`, exported as `ReadGraphVersion`) --
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
  `localsupervisor/graph_process.go` (`ResolveGraphBinary`) is the sole external
  caller.
- `VersionReader` -- the function type callers implement to report a
  NornicDB binary's version without this package executing anything itself.

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/eshulocal` -- `ResolveHomeDir`, used to locate Eshu's managed
  home for the installed binary and manifest paths. This package passes it
  both `os.Getenv` and `os.UserHomeDir`, so the variables it can consult are:
  `ESHU_HOME` when set (used verbatim with `~` expanded, and with **no**
  `eshu` segment appended, unlike every fallback below); otherwise `HOME`
  alone on macOS (`~/Library/Application Support/eshu`), `XDG_DATA_HOME`
  then `HOME` on Linux (`$XDG_DATA_HOME/eshu` or `~/.local/share/eshu`), and
  `LOCALAPPDATA` then `USERPROFILE` on Windows (`%LOCALAPPDATA%\eshu` or
  `%USERPROFILE%\AppData\Local\eshu`). `HOME`/`USERPROFILE` count as env
  reads because that is how `os.UserHomeDir` is defined
- `internal/query` -- `GraphBackendNornicDB`, the backend name recorded in
  the install manifest
- `internal/buildinfo` -- `AppVersion`, used to resolve the pinned release
  manifest entry for the running Eshu version
- Consumed by `go/cmd/eshu`: the `install nornicdb` wrapper
  (`graph_install_cmd.go`), `graph.go`'s `eshu graph upgrade` path
  (`localsupervisor.UpgradeForLayout`), and `graph_process.go`'s
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

## Performance and observability of the extraction

This package arrived by moving files out of `go/cmd/eshu`, not by changing what
they do, so there is no before/after measurement to report and claiming one
would be dishonest. What follows is what was actually established.

No-Regression Evidence: the extraction is behaviour-preserving, proven by
running the same five invocations against a binary built from the base commit
and one built from this branch -- `install nornicdb --help`, `graph upgrade
--help`, `install --help`, a missing-source error path, and a
conflicting-flags error path. Combined stdout and stderr are byte-identical
across all five and the exit codes match (0, 0, 0, 1, 1). `go test
./cmd/eshu/... ./internal/cli/... -count=1` passes, and no `testdata/` path
appears in the diff, so the golden-corpus gate (B-7, which indexes 20 real
repositories and compares against a saved snapshot) and the end-to-end
snapshot (B-12) are untouched.

The one shape change is that `readVersion` is now injected as a
`VersionReader` parameter instead of being a package-level call. That replaces
a direct function reference with an indirect one on a path that runs at most a
few times per `eshu install nornicdb` invocation, each of which already forks a
subprocess to read a version string. There is no loop and no hot path here; the
cost is not measurable against a process spawn.

No-Observability-Change: this package emits no metrics, spans, or logs, and the
move neither added nor removed an instrument. Operator-visible output is
unchanged, which is what the byte-identical parity captures above demonstrate.
`ManagedBinaryIfPresent` still returns an unwrapped `os.Stat` error on purpose
so `os.IsNotExist` keeps matching it -- see the invariant in `AGENTS.md`.

## Related docs

- `docs/public/reference/graph-backend-installation.md` -- the `eshu install
  nornicdb` operator guide (source kinds, `--force`, `--sha256`)
- `docs/public/reference/environment-ingestion-queues.md` -- documents
  `ESHU_NORNICDB_INSTALL_TIMEOUT`, this package's install-source download
  timeout. That is one of two environment reads here; the other is the
  managed-home lookup through `eshulocal.ResolveHomeDir`, which honours
  `ESHU_HOME` (see Dependencies above). Neither is process wiring -- both
  scope an install, and everything else arrives as a parameter.
