# AGENTS.md - scanner-component-image guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/internal/collector/scannerworker/component/README.md`
4. `go/internal/collector/scannerworker/AGENTS.md`

## Invariants

- Keep this binary a thin stdio transport over `component.Runner`. Analyzer
  logic belongs in `imageanalyzer`; contract logic belongs in `component`.
- Never print claim values (scope IDs, image references, paths, digests) to
  stdout or stderr. The runner's errors and log lines are already scrubbed;
  do not add new output paths.
- Stdout carries the result document alone. Anything else breaks the host's
  strict decode.
- Do not add store handles, network clients, or claim loops here. The host
  owns the claim; this binary executes one analysis per call.

## Common changes

- A new analyzer needs a new binary package following this one, a component
  ID and manifest in `component`, and a smoke test that builds and drives
  the binary over stdio.
