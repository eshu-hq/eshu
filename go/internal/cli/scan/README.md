# CLI Scan Family

## Purpose

Indexes a local source and decides whether the result is queryable. `Execute`
runs `eshu-bootstrap-index` against a resolved workspace root, then polls the
pipeline status until the queue is drained and healthy. The distinction the
package exists to protect: a bootstrap child that exited zero has *submitted*
work, not finished it, and reporting that as success is what `eshu scan` used
to be asked not to do.

## Ownership boundary

Owns the scan request shape (`Options`, `Target`), the scan result envelope
(`Result` and its `Timings`/`Evidence` blocks), the pipeline status types the
family reads, and the readiness rule (`EvaluateReadiness`).

Does not own process wiring. `go/cmd/eshu/scan.go` is `package main`, so
nothing here can import it; every process-facing collaborator arrives through
`Runtime`. Flag parsing, cobra stream resolution, the JSON envelope, and the
exit-code mapping all stay in that wrapper. See `doc.go` for the contract and
the exact environment this package does read.

## Exported surface

| Symbol | What callers use it for |
| --- | --- |
| `Execute` | Run one scan end to end |
| `Runtime`, `Client` | The process seams `Execute` needs, and the API read surface |
| `Options`, `Target` | The resolved request; `BootstrapArgs`/`BootstrapEnv` build the child's argv and environment |
| `WaitFlag` | The `--wait` flag's name, registered by `go/cmd/eshu/scan.go` and printed by `internal/cli/vulnscan` in its not-ready message, so the string has one owner |
| `Result`, `Timings`, `Evidence` | The result envelope |
| `PipelineStatus` and its `Health`, `Queue`, `GenerationHistory`, `StageSummary`, `DomainBacklog`, `ScopeActivity` | The status report shape |
| `EvaluateReadiness`, `ReadinessVerdict` | The readiness rule, reused by first-run and hosted verification |
| `FetchPipelineStatus`, `FetchQueryProbe` | Production values for the two `Runtime` read seams |
| `ReposDir`, `ResolveTarget`, `TargetKind` | Path and workspace resolution |
| `Truth`, `CurrentGraphBackend` | The truth envelope every scan-derived answer carries |

`doc.go` carries the godoc contract; this table is a map, not a duplicate of it.

## Dependencies

One internal package: `go/internal/eshulocal`, for workspace-root detection
(`ResolveWorkspaceRoot`) and the managed-home layout (`BuildLayout`).
Everything else is stdlib. `eshulocal` pulls a large transitive tree
(`embedded-postgres`, `pgx`) into the build graph; this package touches none of
it and opens no database connection.

`Client` is a consumer-side interface with one method, deliberately narrow so
`go/cmd/eshu`'s `*APIClient` satisfies it without an adapter.

## Telemetry

None. This package emits no metrics, spans, or logs. Operator-visible output is
whatever the CLI wrapper prints, plus the bootstrap child's own stdout and
stderr, which `Execute` streams straight through to the writers it was handed.

## Gotchas / invariants

- **`Execute` returns a usable `Result` alongside an error.** `newResult` seeds
  `Status: "failed"`, so an early return never reads as success, and the
  wrapper still has a status report and evidence to render.
- **A missing `Runtime` seam is an error, not a panic.** `Execute` checks
  `Client`, `Environ`, `LookPath`, `RunBootstrap`, `FetchStatus`, and
  `FetchQueryProbe` before doing any work and names the missing field. A nil
  `Environ` is rejected specifically because an empty base environment silently
  strips `PATH` from the bootstrap child.
- **`Options.RuntimeEnv` replaces the base environment rather than merging with
  it.** A caller supplying it (the vuln-scan local runtime) is describing a
  complete, deliberately isolated child. The scan overrides still win.
- **The readiness deadline runs from the scan's start, not the poll loop's.**
  The bootstrap child's runtime counts against `Options.Timeout` instead of
  resetting it.
- **`AllowPartial` only downgrades a failure once a health state was observed.**
  With no health state there is nothing to call partial, and the error stands.
- **Error text is an operator contract.** Several returns carry a
  `//nolint:wrapcheck` because wrapping them would change what `eshu scan`
  prints. `go/cmd/eshu` is excluded from `wrapcheck` in `go/.golangci.yml` and
  `go/internal/cli` is not, so code moved here attracts wrap suggestions that
  the CLI's byte-level output cannot absorb.

## Related docs

- `go/cmd/eshu/scan.go` — the cobra wrapper and `defaultScanRuntime`
- `docs/internal/design/package-restructure.md` — why `cmd/eshu` extracts to
  `internal/cli/<family>` at all, and the `package main` constraint that shapes
  `Runtime`
