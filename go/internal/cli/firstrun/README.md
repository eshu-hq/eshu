# firstrun

## Purpose

Logic for `eshu first-run` and `eshu first-run report`, moved out of
`go/cmd/eshu` (issue #6059) so it can be unit-tested from outside the binary.
It owns runtime-shape detection, non-destructive runtime verification, the
index-or-reuse step, the bounded first query, onboarding-failure
classification, and the redacted first-run evidence report.

## Ownership boundary

This package owns the orchestration and the evidence model. It does NOT own
process contact: cobra flags, the API client, the repository-list wire decode,
the repository selector matcher, PATH lookup, the config-backed MCP endpoint,
and the scan runtime are resolved by the cobra wrapper in
`go/cmd/eshu/first_run.go` and passed in through `Deps`. Credential redaction
is NOT owned here either — the rules live in `internal/cli/evidredact` (over
`internal/urlredact`), and this package only calls them. The onboarding
benchmark (`first-run-benchmark`) is a separate family that stays in
`go/cmd/eshu` and imports this package's `Result` vocabulary.

## Exported surface

`Execute` runs the steps and returns the canonical `Result`; `NewResult`
builds the truthful-until-proven initial state and `RenderHuman` prints the
operator summary. `BuildEvidence` projects a `Result` into an
`EvidenceReport`; `RenderEvidenceArtifact`, `RenderEvidenceTerminal`, and
`WriteEvidenceArtifact` render it, with `NormalizeEvidenceFormat` and the
`EvidenceFormat*` constants naming the accepted formats. `Deps`, `Options`,
`RuntimeProbe`, `Repository`, and `RepositoryList` are the seam types the
wrapper fills. `Diagnostic`, `FailureClass`, and the `Class*` constants are
the classified-failure vocabulary carried in `Result` and `EvidenceReport`.
`APIHealthy`, `QuoteIfEmpty`, `Truth`, and `QueryEndpoint` back the wrapper's
production wiring. See `doc.go` for the godoc contract.

## Dependencies

`internal/cli/scan` (readiness evaluation, scan execution seams, the
`scan.Client` read interface, and truth labels), `internal/cli/evidredact`
(redaction rules), and `internal/cli/apierr` (HTTP status classification for
the auth-mismatch diagnostic).

## Telemetry

None. The command's observable surface is its stdout/stderr rendering, the
JSON envelope, and the evidence artifact; the API and pipeline it probes carry
their own telemetry.

## Gotchas / invariants

- `Execute` never reports success unless the bounded query returned; process
  or readiness health alone is never success.
- Every endpoint, path, and free-form field is redacted before it enters
  `EvidenceReport`, so the renderers and the on-disk artifact only see
  redacted data. Keep new fields behind the same scrubs; the composed-string
  redaction tests in this package and the corpus in `internal/urlredact` are
  the guard.
- `Truth` reads `ESHU_GRAPH_BACKEND` through `scan.CurrentGraphBackend` — an
  env read that is invisible at the call site.
- A nil `Deps.MatchesSelector` matches nothing, so a miswired caller falls
  back to a fresh scan instead of reusing an unproven index; a nil
  `Deps.ResolveMCPEndpoint` reads as "no MCP endpoint configured".
- The wrapper keeps the envelope decode (`parseFirstRunEnvelope`) because the
  benchmark family shares it; this package only consumes the decoded `Result`.

## Related docs

`docs/public/reference/first-run-evidence.md` explains how to read the
artifact and its redaction limits.
