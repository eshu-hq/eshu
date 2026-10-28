# AGENTS.md - scanner-component-sbom guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/internal/collector/scannerworker/component/README.md`
4. `go/internal/collector/scannerworker/AGENTS.md`

## Invariants

- Keep this binary a thin stdio transport over `component.Runner`. Analyzer
  logic belongs in `sbomgenerator`; contract logic belongs in `component`.
- Never print claim values (scope IDs, image references, paths, digests) to
  stdout or stderr. The runner's errors and log lines are already scrubbed;
  do not add new output paths.
- Stdout carries the result document alone. Anything else breaks the host's
  strict decode.
- Do not add store handles, network clients, or claim loops here. The host
  owns the claim; this binary executes one analysis per call.

## Common changes

- Analyzer changes belong in `sbomgenerator` (including the repository-walk
  source); binary changes stay transport-only.
