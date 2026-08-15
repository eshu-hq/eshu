# Agent Instructions: internal/cli/vulnscan

Scoped rules for `go/internal/cli/vulnscan`. The root `AGENTS.md` and
`CLAUDE.md` still apply.

## Read first

1. [`doc.go`](doc.go) — the package contract and the two seams that exist
   because their concrete types live in package main.
2. [`README.md`](README.md) — ownership boundary, invariants, and why
   `Result.Scan` is `any`.
3. [`go/cmd/eshu/vuln_scan.go`](../../../cmd/eshu/vuln_scan.go) and
   [`vuln_scan_provider_parity.go`](../../../cmd/eshu/vuln_scan_provider_parity.go)
   — the cobra wrappers, and the only consumers.

## Invariants

- **Fail closed.** A clean `ready_zero_findings` answer requires the readiness
  envelope to prove advisory and package-registry evidence covering the observed
  dependencies is present and fresh. Any change that lets a `ready_*` verdict
  through with unproven evidence is a product bug, not a relaxation.
- **The server's verdict wins on non-ready states.** Do not add CLI-side reasons
  on top of `not_configured`, `target_incomplete`, `evidence_incomplete`, or
  `readiness_unavailable`.
- **No cobra, no process environment for decisions, no exit codes.** Flags,
  streams, `os.Getenv` lookups that drive behavior, and the mapping onto
  `commandExitError` all belong in `go/cmd/eshu`. The one environment read here
  is `os.Getenv` passed to `localsupervisor.ChildOverrides` when composing a
  child process environment, which is how `localsupervisor` composes its own.
  Guard the rule with
  `go list -deps ./internal/cli/vulnscan | rg spf13` — it must print nothing.
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
  the scanner-exit predicate in `go/cmd/eshu/vuln_scan.go`, which lists the
  codes a report is still written for.
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
