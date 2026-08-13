# AGENTS.md — go/internal/cli/graphinstall guidance for LLM assistants

## Read first

1. `go/internal/cli/graphinstall/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/graphinstall/doc.go` — the godoc contract
3. `go/cmd/eshu/graph_install_cmd.go` — the cobra `RunE` wrapper that
   resolves process state (flags, `localGraphReadVersion`) and calls into
   this package. This is the file that shows how the two halves fit
   together.
4. `go/cmd/eshu/local_graph_process.go` — the other caller
   (`resolveNornicDBBinary`), and the home of `readLocalGraphVersion`, the
   subprocess-execution logic this package deliberately does not have.

## Invariants this package enforces

- **No process wiring in this package.** No cobra flags, no
  `os.Getenv` reads beyond `ESHU_NORNICDB_INSTALL_TIMEOUT` (a download
  timeout, not process wiring), no `fmt.Print*`. `go/cmd/eshu` is
  `package main`, so nothing can import it — any symbol that reads a flag or
  maps to an exit code has to live in `graph_install_cmd.go` instead.
- **Never execute a binary.** This package verifies a candidate NornicDB
  binary by calling the `VersionReader` it was handed
  (`Options.ReadVersion` / `ManagedBinaryIfPresent`'s parameter), never by
  running `exec.Command` itself. The real implementation
  (`readLocalGraphVersion`) stays in `go/cmd/eshu/local_graph_process.go`
  because it is part of the local_graph process-supervision cluster, which
  `docs/internal/design/package-restructure.md` documents as a real
  bidirectional cycle that must move as one unit or not at all. If you find
  yourself wanting to add `os/exec` here for anything other than
  `expandPackage`'s `pkgutil --expand-full` call (source.go, darwin `.pkg`
  extraction, not version verification), that logic belongs in
  `go/cmd/eshu`, not here.
- **`Options.ReadVersion` is required.** `Install` and `ManagedBinaryIfPresent`
  both fail fast with a `graphinstall:`-prefixed error when handed a nil
  `VersionReader`, rather than reaching a nil-pointer panic deeper in the
  call chain (`prepareInstallSource` → `inspectInstallSource`).
- **`ManagedBinaryIfPresent`'s `os.Stat` error stays unwrapped.**
  `resolveNornicDBBinary` in `go/cmd/eshu` depends on `os.IsNotExist(err)`
  matching the raw `*os.PathError`; wrapping it with `%w` breaks that check
  silently because `os.IsNotExist` only unwraps `*PathError`/`*LinkError`/
  `*SyscallError` by type switch, not the general `errors.Unwrap` chain. The
  `//nolint:wrapcheck` on that line is intentional — do not "fix" it by
  wrapping.

## Common changes and how to scope them

- **Add a new install source kind** (e.g. a new archive format) → add the
  detection to `looksLikeArchive`/`looksLikePackage` and a case to
  `inspectInstallSource`'s switch in source.go. Why: `installSourceKind` is
  the single closed set `Install` and the tests key off of; a new kind
  handled elsewhere breaks the switch's exhaustiveness.
- **Change what counts as a valid install source (`--from` empty)** → edit
  `resolvePinnedReleaseSource` in release_manifest.go, not `Install`. It is
  the single place that reads the embedded/overridden pinned release
  manifest and resolves an OS/arch/headless match.
- **Change the managed install layout** (`~/.eshu/bin`,
  `~/.eshu/graph-backends/nornicdb/manifest.json`) → edit
  `managedBinaryPath`/`installManifestPath` in install.go. Both call
  `resolveHomeDir`, so a layout change only needs updating in one of these
  two functions plus the path segment, not scattered `filepath.Join` calls.

## Failure modes and how to debug

- Symptom: `Install` returns `graphinstall: Options.ReadVersion is required`
  (or `ManagedBinaryIfPresent` its equivalent) → almost always a test or
  caller that built an `Options{}` literal without `ReadVersion`. Both guards
  run before any other work, so this fails immediately rather than part-way
  through an install. Every `Install`/`ManagedBinaryIfPresent` call in this
  package's own tests passes `execNornicDBVersion` (install_helpers_test.go);
  `graph_install_cmd.go` passes `localGraphReadVersion`.
- Symptom: a test asserting `os.IsNotExist` on `ManagedBinaryIfPresent`'s
  error starts failing after an edit near the `os.Stat` call → check whether
  the edit added `fmt.Errorf("...: %w", err)` around that specific error;
  see the invariant above.
- Symptom: `TestEmbeddedNornicDBReleaseManifestHasNoBareAssetsWhileTrackingMain`
  (release_manifest_test.go) fails after editing
  `nornicdb_release_manifest.json` → that test is a deliberate policy guard:
  Eshu currently tracks the latest NornicDB main branch and ships zero pinned
  releases. Adding a real release entry to the embedded manifest is an
  intentional policy change, not a bug in the test.

## Anti-patterns specific to this package

- **Printing from this package.** `Install` and `ManagedBinaryIfPresent`
  return data and errors; `fmt.Print*` belongs only in
  `graph_install_cmd.go`.
- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only `go/cmd/eshu` has (a cobra flag, the
  real `VersionReader`), add a parameter or a narrow interface instead.
- **Calling `exec.Command` to check a binary's version.** Use the
  `VersionReader` you were handed.

## What NOT to change without an ADR

- The `VersionReader` function type's signature — `graph_install_cmd.go`
  wires `localGraphReadVersion` against it structurally; changing the
  signature breaks that wiring silently until the next build.
- Moving `readLocalGraphVersion`/the local_graph process-supervision cluster
  into this package. `docs/internal/design/package-restructure.md` documents
  it as a real bidirectional cycle (31 files) that has to move as one unit
  or not at all — see the cmd/eshu section and the acyclic-boundary
  discussion there before attempting it.
