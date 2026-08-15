# CLI Vulnerability Scan

## Purpose

This package holds what `eshu vuln-scan repo` and `eshu vuln-scan provider-parity`
actually do. It reads the reducer-owned impact-findings envelope, decides
whether the evidence behind it is good enough to call the answer clean, builds
the vulnerability report, writes the SARIF and VEX exports, and starts the
one-shot local runtime the repo subcommand needs when no service URL is
configured. `go/cmd/eshu/vuln_scan.go` and
`go/cmd/eshu/vuln_scan_provider_parity.go` are the cobra wrappers around it.

The logic lived in `go/cmd/eshu` until #6059. That package is `package main`,
so nothing could import it and none of this was reachable from a test outside
the binary.

## Ownership boundary

This package owns the vuln-scan decision and output surface: scope-plan
derivation, the fail-closed guards, exit classification, the report shape, both
export formats, the local runtime lifecycle, and the provider-parity mapping.

It does not own:

- **Flags, streams, and exit codes.** Reading cobra flags, resolving
  `cmd.OutOrStdout()`, looking up `ESHU_SERVICE_URL` or a provider token in the
  environment, and turning a failure into a process exit code all stay in
  `go/cmd/eshu`. Every function here takes its inputs as arguments.
- **The scan itself.** `eshu scan` — target resolution, bootstrap, readiness
  waiting — is a separate family, still in `go/cmd/eshu`. Its result reaches the
  envelope through `Result.Scan`, typed `any`, which this package carries and
  never reads.
- **What a finding means.** Impact status, priority, and reachability are the
  reducer's verdicts. Nothing here recomputes them; the guards decide only
  whether the CLI trusts the evidence enough to report a clean answer.

## Exported surface

See [`doc.go`](doc.go) for the godoc contract. In outline:

- **Result path** — `Result`, `Target`, `NewResult`, `FetchImpactFindings`,
  `ReadinessState`, `ApplyScope`, `RecordPerformance`, `EnvelopeFetcher`.
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
- **Local runtime** — `LocalRuntime` and the `PrepareLocalRuntime`,
  `ReserveLocalAPIPort`, `StartLocalOwner`, `StartLocalAPI`, `StopLocalProcess`,
  `WaitLocalAPI` seams.
- **Provider parity** — `ParityOptions`, `ParitySource`, `ParityData`,
  `ParityEvidenceSource`, `ParityEvidenceFromReadiness`, `RenderParitySummary`,
  `EshuSource`, `MapEshuFindings`, `NormalizeProviderName`, `CleanStringSlice`.

## Dependencies

Internal packages: `internal/cli/localsupervisor` and `internal/cli/procexec`
for the local runtime, `internal/eshulocal` for workspace layout and the owner
record, `internal/query` for the profile and graph-backend constants,
`internal/exports` for the shared SARIF snapshot shape, `internal/buildinfo`
for the tool version stamped into a SARIF run, and
`internal/vulnerabilityparity` plus `internal/vulnerabilityparityproof` for the
parity comparison.

Cobra is not among them, directly or transitively —
`go list -deps ./internal/cli/vulnscan | rg spf13` prints nothing. Transport is
the `EnvelopeFetcher` interface, not the CLI's concrete API client.

## Telemetry

None. This package emits no metrics, spans, or structured logs — `rg
'telemetry\.|tracer\.Start|slog\.'` over the directory matches nothing. Its
observable output is the CLI's own: the JSON envelope's `scan_performance`
block (wall time, repository footprint, per-family fact counts, cache
freshness, stop threshold), the `scope_plan` block naming which guard fired,
and the process exit code.

## Gotchas / invariants

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

## Related docs

- [`go/cmd/eshu/README.md`](../../../cmd/eshu/README.md) — the CLI wrapper and
  the rest of the command families.
- [`go/internal/cli/localsupervisor/README.md`](../localsupervisor/README.md) —
  the owner record, child environment composition, and process supervision this
  package builds on.
- [Local Testing](../../../../docs/public/reference/local-testing.md) — the
  gates that cover this package.
