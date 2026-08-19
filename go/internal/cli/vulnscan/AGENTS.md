# Agent Instructions: internal/cli/vulnscan

Scoped rules for `go/internal/cli/vulnscan`. The root `AGENTS.md` and
`CLAUDE.md` still apply.

## Read first

1. [`doc.go`](doc.go) — the package contract, the two seams that exist
   because their concrete types live in package main, and the scan-runtime
   seam that exists to keep process contact in the wrapper.
2. [`README.md`](README.md) — ownership boundary, invariants, which document
   each outcome writes, and why `Result.Scan` is `any`.
3. [`run.go`](run.go) and [`finish.go`](finish.go) — `RunRepo`, the repo
   subcommand end to end, and the output-selection rules behind it.
4. [`go/cmd/eshu/vuln_scan.go`](../../../cmd/eshu/vuln_scan.go) — the cobra
   wrappers for both `vuln-scan repo` and `vuln-scan provider-parity`, and the
   only consumers.

## Invariants

- **Fail closed.** A clean `ready_zero_findings` answer requires the readiness
  envelope to prove advisory and package-registry evidence covering the observed
  dependencies is present and fresh. Any change that lets a `ready_*` verdict
  through with unproven evidence is a product bug, not a relaxation.
- **The server's verdict wins on non-ready states.** Do not add CLI-side reasons
  on top of `not_configured`, `target_incomplete`, `evidence_incomplete`, or
  `readiness_unavailable`.
- **No cobra, no process environment for decisions, no exit codes.** Flags,
  the process streams, `os.Getenv` lookups that drive behavior, and the mapping
  onto `commandExitError` all belong in `go/cmd/eshu`. `RunRepo` returns a
  `*Failure` for a scanner verdict and a plain error otherwise; it writes only
  to the writers in `RepoDeps`. The one direct environment read here is
  `os.Getenv` passed to `localsupervisor.ChildOverrides` when composing a
  child process environment, which is how `localsupervisor` composes its own;
  the truth fallback in `finishRepo` also reads `ESHU_GRAPH_BACKEND` through
  `scan.CurrentGraphBackend`, as provenance. Guard the rule with
  `go list -deps ./internal/cli/vulnscan | rg spf13` — it must print nothing —
  and with `doc_lockstep_test.go`, which pins the direct imports and the `os`
  and `fmt` calls; widen its sets only alongside the docs.
- **The exit codes 3/4/5 and their messages are the published contract, and
  the failure paths are the proof.** A change to `RunRepo` or `finishRepo` is
  proven by `TestRunRepoOutcomeContract`, which drives the real `RunRepo`
  through every verdict and unclassified-error class and pins code, message,
  readiness state and envelope error member, and by
  `TestRunRepoOutputSelectionByOutcome`, which pins which document each output
  mode writes for codes 3, 4 and 5 versus an unclassified error, plus the
  wrapper's own tests in `go/cmd/eshu`. A happy-path test proves almost
  nothing here.
- **The JSON envelope and the report are a wire contract.** Field names, field
  order, and `omitempty` are what operators and their tooling parse. Changing
  the report shape needs a new `ReportSchemaVersion`, not an edit to the
  existing one.
- **Export output is byte-stable.** SARIF and VEX sort their lists. Keep the
  `cloneAndSortStrings` / `sortedEvidenceHandles` calls when adding a field.

## Common changes

- **New readiness evidence family:** extend `BuildScopePlan`'s switch and, if it
  gates a clean answer, `ApplyScopedGuards`. Add the corresponding constant
  next to `MissingPackageRegistryMetadata` and cover both the guard firing and
  not firing.
- **New report field:** add it to the type in `report.go` with a JSON tag, read
  it in `buildReportFindings`, and decide whether SARIF and VEX carry it too.
  Three writers read the same finding map; a field added to one and not the
  others is the drift this package is easiest to get wrong on.
- **New exit class:** `ExitClassification` and `ExitMessage` in `exit.go`, and
  `isScannerExit` in `finish.go`, which lists the codes a report is still
  written for. Add the new class to `TestRunRepoOutcomeContract` with its
  message.
- **New step in the repo run:** add it to `RunRepo` between the existing
  steps, give its failure the readiness state the envelope should report, and
  return through `finishRepoAfterCleanup` so the local runtime is stopped and
  the document is still written. A new process dependency goes in `RepoDeps`,
  wired by the wrapper, not read here.
- **Local runtime change:** the seams are package variables so tests can drive
  startup without binding ports. Keep new steps behind a seam, keep that seam
  unexported (`PrepareLocalRuntime` is the only one the wrapper reads), and keep
  every post-start failure path calling `stopLocalRuntime`.

## Failure modes seen here

- A guard that cannot fail. A scope guard test must fail when the guard is
  removed from the production path — prove it by breaking `ApplyScopedGuards`,
  not a copy of it.
- Evidence counted from a family with facts but stale freshness.
  `readinessEvidenceFamilies` requires both; dropping the freshness check makes
  a stale cache read as full coverage.
- A VEX `not_affected` invented from an impact status the reducer never gave.
  An unmapped status must produce no statement.

## Do not change without owner review

- The exit-code numbers (0/3/4/5). They are the published scanner contract and
  CI pipelines branch on them.
- `ReportSchemaVersion` and `VEXSchemaVersion`.
- The `--broad` semantics — specifically that it does not relax the
  package-registry guard.
- `Result.Scan`'s type. Making it concrete pulls the scan family's type tree
  across a boundary that is deliberate; see `README.md`.
