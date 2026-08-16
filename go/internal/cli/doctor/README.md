# CLI Doctor

## Purpose

`doctor` owns the report behind `eshu doctor` — the local-environment check an
operator runs first when a stack will not come up. It answers "is this machine
configured the way Eshu expects?" and nothing else.

Six checks, in the order an operator needs them:

| Check | Reports |
| --- | --- |
| Config directory | exists / missing, with the resolved path |
| Settings file | exists / missing, with the resolved path |
| Service binaries | each of the five Eshu executables found on `PATH` or not |
| API health | `/health` reachable, healthy, or the status it returned |
| Graph Bolt URI | configured (redacted) or not configured |
| Postgres DSN | configured or not — presence only, never the value |

## Why every check is advisory

`Run` returns `nil` even when every check fails. An operator running `doctor`
already knows something is wrong; stopping at the first problem would hide the
rest, and the combination is usually what identifies the cause. A missing
binary plus an unreachable API is a different story from a missing binary and a
healthy API.

## The report is a redaction surface

This is the part to keep in mind before adding a line. Doctor output is the
first thing an operator pastes into a bug report or a support ticket, so
anything it prints is effectively published.

- **The Bolt URI carries its password in userinfo.**
  `bolt://neo4j:PASSWORD@graph.example.com:7687` is the canonical form, and the
  repo's own screen-shape evidence uses exactly it. The value is written
  through `evidredact.Endpoint`, which replaces the userinfo and strips the
  query and fragment. The host survives on purpose — an operator needs to
  recognise *which* backend was configured, and the host is not the secret.
- **The API base URL goes through the same call**, because an
  operator-configured URL can carry a token in its query string.
- **The Postgres DSN is never printed.** It is credentials end to end, and
  unlike the Bolt URI there is no host-shaped remainder worth showing.

`doctor_redaction_test.go` is the screen. Its sentinel is planted inside a
value rather than at a token boundary, so a check that only matches whole
fields cannot pass by accident.

This package exists because that leak was real: before the extraction the
report printed `NEO4J_URI` verbatim from `package main`, where nothing could
test it.

## Ownership boundary

No process wiring lives here. No cobra, no environment reads, no process
streams, no `os.Exit`. The report goes to the `io.Writer` the caller supplies.

Filesystem and `PATH` access go through `Deps`, so a test can describe a broken
machine — no config directory, no binaries, an API that times out — without
being run on one. `Deps` fields left nil fall back to `os.Stat`,
`exec.LookPath`, and an HTTP client bounded by a 3-second timeout.

`TestPackageStaysProcessNeutral` enforces the boundary by parsing this
directory: it fails on a cobra import or on any process-bound `os`/`fmt`
selector. `os.Stat` and `os.FileInfo` are deliberately allowed — `os` is a
legitimate dependency for the `Deps` seam, and it is the process-bound
selectors that are banned. That distinction is why a `go list -deps` scan
cannot do this job.

## What stays in `go/cmd/eshu/doctor.go`

`go/cmd/eshu` is `package main`, so nothing can import it. The wrapper resolves
process state and passes it in as plain values:

- `NEO4J_URI` from the environment, falling back to the settings file via
  `cliconfig.ResolveValue`
- the config directory and settings-file path via `cliconfig`
- the API base URL from the API client
- `ESHU_POSTGRES_DSN` from the environment
- `cmd.OutOrStdout()` as the destination

`doctor_cmd_test.go` pins that wiring end to end by capturing the command's
output and asserting a credential-bearing Bolt URI does not survive it.

## Adding a check

Add the value to `Deps`, resolve it in the cobra wrapper, and render it in
`Run`. Then ask the question this package exists for: **can the value carry a
credential?** If it can, route it through `evidredact.Endpoint` (for anything
URL-shaped) or report presence only, and add a sentinel case to
`doctor_redaction_test.go`. A new line that prints an operator-supplied value
verbatim is the exact defect this package was extracted to fix.
