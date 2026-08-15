# AGENTS.md — go/internal/cli/procexec guidance for LLM assistants

## Read first

1. `go/internal/cli/procexec/README.md` — purpose, exported surface, the two
   behaviours that surprise people
2. `go/internal/cli/procexec/doc.go` — the godoc contract
3. `go/cmd/eshu/service.go` — `runMCPStart`, the densest caller: it uses all
   five seams plus both helpers in one function
4. `go/cmd/eshu/watch_test.go` and `go/cmd/eshu/service_local_test.go` — how
   callers substitute the seams; the shape any change here has to keep working

## Invariants this package enforces

- **`Exec` does not return on success.** It calls `syscall.Exec`, which
  replaces the running process image. Nothing after the call runs — no
  deferred function, no flush, no cleanup. Any output a caller wants the user
  to see must be written before the call. This is not a style point; it is why
  the package exists.
- **The five seams stay package-level variables.** `Executable`, `Getwd`,
  `LookPath`, `Exec`, `Environ`. Turning any of them into a plain function
  compiles fine here and breaks every override site in `go/cmd/eshu` at once —
  and for `Exec` it makes the re-exec paths untestable, because a test that
  reached the real `syscall.Exec` would lose its own process rather than fail.
  `procexec_test.go`'s `TestExecSeamIsWiredAndSubstitutable` and
  `TestSeamsAreSubstitutableAndRestorable` guard this.
- **They are a test seam, not configuration.** Production code assigns to them
  nowhere. A test that assigns one must restore the original (`t.Cleanup` or
  `defer`) and must not run in parallel with another test touching the same
  seam — they are process-global.
- **No process wiring in this package.** No cobra flags, no `fmt.Print*`, no
  exit codes. `go/cmd/eshu` is `package main`, so nothing can import it; a
  symbol that reads a flag or maps a result to an exit code belongs there.
  Verify the negative with `go list -deps ./internal/cli/procexec | rg spf13`
  returning nothing, not by reading this file.
- **Standard library only.** `go list -deps` currently reports 61 packages,
  all stdlib. Adding an Eshu dependency here would re-couple the four command
  families this package was extracted to decouple.

## Common changes and how to scope them

- **A new re-exec call site in `go/cmd/eshu`** → call `procexec.Exec` with
  `procexec.CleanExecutableArg0(binary)` as `args[0]` and a
  `procexec.MergeEnvironment(procexec.Environ(), overrides)` environment. Do
  not reintroduce a local `os.Environ()` or a hand-built `argv[0]`; that is the
  duplication this package removed.
- **A new host-state dependency that a test must stub** → add a seam variable
  here following the same shape, and add it to the two seam tests. Do not add a
  bare function and expect callers to work around it.
- **A change to `MergeEnvironment`'s splitting or precedence** → the exact
  rules (first `=` only, no-`=` entries dropped, last repeated name wins,
  empty-string override is an assignment) are each pinned by a case in
  `environment_test.go`. Change the test and the doc comment in the same edit,
  and say why in the commit message — these rules are what child processes
  inherit.

## Failure modes and how to debug

- Symptom: a `go/cmd/eshu` test hangs, exits mid-run, or the test binary
  vanishes → a re-exec path reached the real `procexec.Exec`. The test did not
  substitute it, or substituted it and a prior test's `t.Cleanup` restored the
  original first. Check that the assignment happens before the code under test
  runs, and that no two tests touching `Exec` run in parallel.
- Symptom: a child process is missing an environment variable the parent
  clearly had → check the entry actually contained an `=`.
  `MergeEnvironment` drops an entry with no `=` at all, silently, because such
  an entry has no name to key on.
- Symptom: a child reports its own name as `.` or as a directory name →
  `CleanExecutableArg0` was handed an empty path or one ending in a separator.
  The `"eshu"` fallback does not cover either; see the pinned cases in
  `procexec_test.go`.
- Symptom: a test asserts on the order of `MergeEnvironment`'s result and
  fails intermittently → the result is built from a map and has no defined
  order. Sort before comparing.

## Anti-patterns specific to this package

- **Calling `syscall.Exec` directly from a caller.** It bypasses the seam and
  makes that path untestable in-process.
- **Assigning a seam from production code.** They exist for tests. A
  production need to vary behaviour is a parameter, not a global assignment.
- **Assuming a green `GOOS=windows` build means it works.**
  `syscall/exec_windows.go` defines `Exec` as a stub returning `EWINDOWS`, so
  it compiles and then always fails at runtime.
- **Adding a dependency on another Eshu package.** See the invariant above.

## What NOT to change without an ADR

- The `Exec` signature `func(binary string, args []string, env []string) error`.
  Six call sites in `go/cmd/eshu` and six test files wire against it
  structurally; a signature change breaks all of them at once and there is no
  compile-time bridge.
- Making the seams fields on a struct or parameters. That was considered and
  rejected during the extraction (#6059): the call sites are cobra `RunE`
  functions with signatures cobra fixes, so an instance or parameter would just
  reintroduce a package-level default in `go/cmd/eshu` — the same mutable
  global with an extra layer, and none of the coupling this move removed.
