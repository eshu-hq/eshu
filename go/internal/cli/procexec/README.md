# go/internal/cli/procexec

The shared "resolve the eshu binary, build its environment, hand the process
over to it" seam for `go/cmd/eshu`.

## Why this is a package

Six files in `go/cmd/eshu` re-exec a binary, and they belong to four unrelated
command families: `eshu watch` (`basic.go`), `eshu scan` (`scan.go`),
`eshu vuln scan --local` (`vuln_scan_local.go`), and `eshu graph start`
(`graph.go`), plus `service.go`'s MCP start paths and
`local_graph_process.go`'s NornicDB launcher.

Before this package existed, all six reached these symbols through
`go/cmd/eshu`'s package scope, which meant `basic.go` and `scan.go` depended on
the `local_host`/`local_graph` supervisor cluster for nothing more than an
`os.Environ` call. `local_host_config.go` and `service.go` referenced each
other for the same reason. Moving these seven symbols out removed both.

## Exported surface

| Symbol | Kind | What it does |
| --- | --- | --- |
| `Executable` | seam var | Path of the running binary. Defaults to `os.Executable`. |
| `Getwd` | seam var | Process working directory. Defaults to `os.Getwd`. |
| `LookPath` | seam var | Resolve a binary name against `PATH`. Defaults to `exec.LookPath`. |
| `Exec` | seam var | Replace the process image. Defaults to `syscall.Exec`. |
| `Environ` | seam var | Process environment as `name=value`. Defaults to `os.Environ`. |
| `CleanExecutableArg0` | func | Reduce a binary path to the `argv[0]` a child should see. |
| `MergeEnvironment` | func | Layer overrides onto a `name=value` slice. |

## `Exec` does not return

`Exec` calls `syscall.Exec`, which replaces the running process image. On
success **the call never returns**: the calling program ceases to exist, no
deferred function runs, no buffered output is flushed, and the replacement
inherits the caller's PID and file descriptors. It returns only when the exec
itself fails. Anything a caller wants to happen — a printed message, a flushed
writer, a cleanup — has to happen *before* the call, not after it.

That is also why the five seams are variables rather than functions. A test
that reached a real `syscall.Exec` would not fail; the test binary would be
gone. Callers route through variables so a test can substitute them, which is
exactly what `go/cmd/eshu`'s `watch_test.go`, `graph_test.go`,
`service_local_test.go`, `service_mcp_http_auth_test.go`, `local_host_test.go`,
and `vuln_scan_local_test.go` all do.

`syscall.Exec` compiles on every `GOOS`, windows included, but the windows
implementation (`syscall/exec_windows.go`) is a stub returning `EWINDOWS`. A
green `GOOS=windows` build says nothing about whether process replacement
works there.

## Two behaviours worth knowing before you change them

**`MergeEnvironment`** splits each base entry on its **first** `=`, so a value
may contain further `=` characters (`ESHU_MCP_AUTH=user=pass` keeps
`user=pass`). An entry with no `=` at all carries no name and is dropped. When
the base repeats a name the last occurrence wins. An override to the empty
string is a real assignment, not a deletion. The result is built from a map, so
entry order is unspecified and varies between calls — `exec` does not care, and
a caller that does must sort.

**`CleanExecutableArg0`**'s `"eshu"` fallback covers less than it looks like it
does, because `filepath.Base` runs first. It fires only for a whitespace-only
input. An empty input yields Base's `"."`, and a trailing separator yields the
parent directory name (`"/usr/local/bin/"` gives `"bin"`). Neither is reachable
from a successful `Executable()` or `LookPath()`, which is why it has never
mattered — but `procexec_test.go` pins both so a future tidy-up of the fallback
cannot change `argv[0]` silently.

## Boundary

No cobra flags, no printing, no exit codes. `go/cmd/eshu` is `package main`, so
nothing can import it, and any symbol that reads a flag or maps a result to an
exit code stays in the wrapper there. `go list -deps ./internal/cli/procexec`
reports 61 packages, all standard library, with no `spf13/cobra` and no other
Eshu package.

`Environ` and `LookPath` read the process environment and `PATH`. That is the
package's job, not process wiring leaking in.

## Performance and observability of the extraction

This package arrived by moving seven symbols out of `go/cmd/eshu` unchanged,
not by changing what they do, so there is no before/after measurement to
report and inventing one would be dishonest. What follows is what was actually
established.

No-Regression Evidence: the extraction is behaviour-preserving, proven by
running ten CLI invocations against a binary built from the base commit
(`6db694d0628406764ca67a62f4bfdaea0b7b0313`) and one built from this branch,
including the failure paths and the paths that actually reach the re-exec
rather than only the flag parsing above it. Combined stdout and stderr are
byte-identical on all ten and the exit codes match. The harness was proved able
to fail by deliberately mutating the "after" binary and confirming it reported
exactly the mutated case as differing. `go test ./cmd/eshu/...
./internal/cli/... -count=1` passes, and no `testdata/`, cassette, or golden
path appears in the diff, so the golden-corpus gate (B-7) and the end-to-end
snapshot (B-12) are untouched.

There is no shape change to measure. Call sites went from an unqualified
package-scope identifier to a qualified one in another package; both compile to
the same indirect call through a package-level variable, and each one runs at
most once per CLI invocation, immediately before a process spawn or a process
replacement.

No-Observability-Change: this package emits no metrics, spans, or logs, and
the move neither added nor removed an instrument. Every `fmt.Print*` on these
paths stayed in `go/cmd/eshu`, which is what the byte-identical parity captures
above demonstrate.

## Related

- `go/cmd/eshu/AGENTS.md` — the wrapper-stays-thin rule this package serves
- `docs/internal/design/package-restructure.md` — the `cmd/eshu` section, and
  the `local_host`/`local_graph` supervisor cluster this extraction unblocks
- `go/internal/cli/graphinstall`, `go/internal/cli/opdigest` — sibling
  extractions out of the same binary
