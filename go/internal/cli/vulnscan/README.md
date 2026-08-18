# CLI Vulnerability Scan

## Purpose

This package holds what `eshu vuln-scan repo` and `eshu vuln-scan provider-parity`
actually do. `RunRepo` is the repo subcommand once its inputs are resolved: it
runs the scan and waits for readiness, resolves the repository, reads the
reducer-owned impact-findings envelope, decides whether the evidence behind it
is good enough to call the answer clean, stamps the performance block, stops
the one-shot local runtime if the wrapper started one, and writes the JSON
envelope, the SARIF or VEX export, or the human summary. It also holds the
report builder, both export writers, the fail-closed guards, and the local
runtime startup those steps depend on. `go/cmd/eshu/vuln_scan.go` and
`go/cmd/eshu/vuln_scan_provider_parity.go` are the cobra wrappers around it.

The logic lived in `go/cmd/eshu` until #6059. That package is `package main`,
so nothing could import it and none of this was reachable from a test outside
the binary. The report, guards, exports and local runtime moved first; the
orchestration that strung them together followed as `RunRepo`, so the failure
paths -- which verdict maps to which exit code, and which paths still write a
document -- are testable without cobra.

## Ownership boundary

This package owns the vuln-scan decision and output surface: scope-plan
derivation, the fail-closed guards, exit classification, the report shape, both
export formats, the local runtime lifecycle, and the provider-parity mapping.

It does not own:

- **Flags, streams, and exit codes.** Reading cobra flags, resolving
  `cmd.OutOrStdout()`, looking up `ESHU_SERVICE_URL` or a provider token in the
  environment, deciding whether to start the local runtime, building the API
  client, and turning a `Failure` into a process exit code all stay in
  `go/cmd/eshu`. `RunRepo` receives the streams, the client and the scan
  runtime through `RepoDeps` and the resolved flags through `RepoOptions`;
  nothing here reads a flag, calls `os.Exit`, or writes to `os.Stdout`.
- **The scan itself.** `eshu scan` — target resolution, bootstrap, readiness
  waiting — is `go/internal/cli/scan`, and its process seams (PATH lookup, the
  bootstrap child, the inherited environment) are wired by `go/cmd/eshu`.
  `RunRepo` calls `scan.Execute` with the `scan.Runtime` the wrapper hands it
  and starts no bootstrap child of its own. The scan result reaches the
  envelope through `Result.Scan`, typed `any`, which this package carries and
  never reads.
- **What a finding means.** Impact status, priority, and reachability are the
  reducer's verdicts. Nothing here recomputes them; the guards decide only
  whether the CLI trusts the evidence enough to report a clean answer.

## Exported surface

See [`doc.go`](doc.go) for the godoc contract. In outline:

- **Repo run** — `RunRepo`, `RepoDeps`, `RepoOptions`, `RepoClient`. The
  wrapper builds the deps and options and maps the returned `*Failure` onto
  its exit-error type; every other error is returned to the operator as is.
  `RepoClient` is exported because it is the declared type of
  `RepoDeps.Client`, not because a caller names it: the wrapper assigns its
  concrete `*APIClient` there, and the interface exists so the field's contract
  (a plain GET plus the envelope GET) can be read from godoc.
- **Result path** — `Result`, `Target`, `NewResult`, `FetchImpactFindings`,
  `ReadinessState`, `ApplyScope`, `RecordPerformance`, `EnvelopeFetcher`. These
  are the steps `RunRepo` composes.
- **Scope guards** — `ScopePlan`, `BuildScopePlan`, `ApplyScopedGuards`,
  `PackageRegistryMissingEvidence`, `IsReadyReadinessState`, `ResolveScopeMode`,
  `Performance`, `CapturePerformance`, and the `ScopeMode*` / `Missing*`
  constants.
- **Exit contract** — `Failure`, `ExitClassification`, `ExitMessage`,
  `ExitFailure`.
- **Report and output** — `Report` and its member types, `BuildReport`,
  `RenderSummary`, `ReachabilityFromFinding`, `RemediationFromFinding`.
- **Exports** — `WriteSARIF`, `WriteVEX`, `BuildVEXDocument`,
  `RemediationForVEX`, `ExportFormatSARIF`, `ExportFormatVEX`.
- **Local runtime** — `LocalRuntime` and `PrepareLocalRuntime`. The individual
  startup steps are unexported seams; see
  [Gotchas / invariants](#gotchas--invariants).
- **Provider parity** — `ParityOptions`, `ParitySource`, `ParityData`,
  `ParityEvidenceSource`, `ParityEvidenceFromReadiness`, `RenderParitySummary`,
  `EshuSource`, `MapEshuFindings`, `NormalizeProviderName`, `CleanStringSlice`.

## Dependencies

Internal packages: `internal/cli/scan` for the scan `RunRepo` runs first and
the truth envelope it falls back to, `internal/cli/reposelector` for resolving
the scanned root to a repository id, `internal/cli/localsupervisor` and
`internal/cli/procexec` for the local runtime, `internal/eshulocal` for
workspace layout and the owner record, `internal/query` for the profile and
graph-backend constants,
`internal/exports` for the shared SARIF snapshot shape, `internal/buildinfo`
for the tool version stamped into a SARIF run, and
`internal/vulnerabilityparity` plus `internal/vulnerabilityparityproof` for the
parity comparison.

Cobra is not among them, directly or transitively —
`go list -deps ./internal/cli/vulnscan | rg spf13` prints nothing. Transport is
the `RepoClient` interface (a plain GET plus the envelope GET), not the CLI's
concrete API client. `doc_lockstep_test.go` pins the direct import set and the
`os` and `fmt` calls this package makes, so a new dependency or a new piece of
process contact fails a test until the docs name it.

## Telemetry

None. This package emits no metrics, spans, or structured logs — `rg
'telemetry\.|tracer\.Start|slog\.'` over the directory matches nothing. Its
observable output is the CLI's own: the JSON envelope's `scan_performance`
block (wall time, repository footprint, per-family fact counts, cache
freshness, stop threshold), the `scope_plan` block naming which guard fired,
and the process exit code.

## Gotchas / invariants

- **Under `--json`, `RunRepo` writes the envelope for every path that reaches
  the scan; the other outputs are written only for a verdict.** The JSON
  envelope is written for every outcome, with its `error` member set for every
  failure except the findings-present verdict (code 3), which is a successful
  scan that found something. An export (`--export sarif|vex`) and the human
  summary are written for success and for the three scanner verdicts (3, 4, 5)
  and skipped for any other error, because the run never produced a report to
  render. A write failure replaces the outcome. `finishRepo` holds those rules
  and `TestRunRepoOutputSelectionByOutcome` pins them for each of 3, 4 and 5
  against a preflight failure.
- **Local runtime shutdown runs before the document is written and never
  changes the verdict.** A `CloseLocalRuntime` error is appended to
  `Result.Warnings` and printed as a `Warning:` line on the stderr writer; the
  exit outcome is whatever the scan reached.
- **`RepoDeps.StartedAt` is the wrapper's clock, not `RunRepo`'s.** The wrapper
  takes it before starting or attaching to the local runtime, so the
  `scan_performance` wall time still covers that startup as it did when the
  flow lived in `package main`. A zero value means "now".
- **The truth fallback reads one environment variable indirectly.** When a
  path fails before the findings read returns a truth block, `finishRepo`
  builds one with `scan.Truth(..., scan.CurrentGraphBackend())`, and
  `CurrentGraphBackend` reads `ESHU_GRAPH_BACKEND`. That is provenance in the
  envelope, not a decision; the read lives in `internal/cli/scan`.
- **`Result.Scan` is `any` on purpose.** The scan result belongs to the `eshu
  scan` family in package main. Reading it here would pull that whole type tree
  across the boundary. The wrapper assigns it on every path that writes an
  envelope.
- **Guards run only on ready verdicts.** `ApplyScopedGuards` passes
  `not_configured`, `target_incomplete`, `evidence_incomplete`, and
  `readiness_unavailable` through untouched. Shadowing the server's own
  missing-evidence reasons with a CLI reason would hide why the scan stopped.
- **`--broad` relaxes one guard, not both.** It skips the advisory freshness
  check. Package-registry metadata is still required whenever the repository
  has observed dependencies, because that metadata is the join evidence between
  a dependency and an advisory.
- **Unknown freshness fails closed.** A `ready_*` verdict with a freshness value
  that is neither `fresh` nor `stale` is downgraded to `evidence_incomplete`,
  not accepted. The CLI cannot prove the evidence is current, and a clean answer
  it cannot prove is the wrong answer.
- **An unmapped impact status produces no VEX statement.** `vexStatus` returns
  empty for a status it does not know, and the caller skips the finding.
  `VEXStatementPolicy.NonStatementReadiness` tells a consumer that an absent
  statement means "not established", never "not affected".
- **Exit code 0 requires `ready_zero_findings`.** Everything else is 3 (findings
  present), 4 (evidence not established), or 5 (unsupported target evidence).
  An unrecognized state falls back to the finding count, and to 4 when there are
  none.
- **Export writers refuse rather than truncate.** An unresolved repository id or
  an unparseable `generated_at` fails the write; a scopeless SARIF run is worse
  for the ingesting tool than no file.
- **The local runtime never half-starts.** Every failure after a child has
  started stops what it started. Attaching to an existing owner is refused —
  not replaced — when the workspace, Postgres socket, profile, or graph backend
  does not match, because the answer depends on which service was read.
- **The startup steps are unexported on purpose.** Port reservation, owner
  start, API start, process stop, and the health wait are package variables
  (`reserveLocalAPIPortFn` and friends) so this package's own tests can drive
  startup without binding a port or spawning a process. They stay unexported
  because nothing outside substitutes them, and an exported one would let any
  importer swap the path that spawns the local service owner.

## Related docs

- [`go/cmd/eshu/README.md`](../../../cmd/eshu/README.md) — the CLI wrapper and
  the rest of the command families.
- [`go/internal/cli/localsupervisor/README.md`](../localsupervisor/README.md) —
  the owner record, child environment composition, and process supervision this
  package builds on.
- [Local Testing](../../../../docs/public/reference/local-testing.md) — the
  gates that cover this package.
