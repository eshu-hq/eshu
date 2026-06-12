# resolutionparity

Per-language call-resolution accuracy goldens and the CI parity gate for
code-edge resolution provenance (issue #2226, part of epic #2218).

## What it does

Code-call edges carry a `resolution_method` from the closed
[ADR #2222](../../../docs/internal/design/2222-resolution-provenance-code-edges.md)
vocabulary (emitted by the reducer in #2223). This package measures, per
language, how the resolver distributes calls across those tiers and pins the
distribution as a golden so a parser or resolver regression that shifts the
tiers fails CI.

`TestResolutionTierGoldens`:

1. Parses every matching file in a language's `tests/fixtures/sample_projects`
   corpus with the real `parser.DefaultEngine()`.
2. Injects deterministic synthetic uids onto parsed entities (the ingester
   assigns real uids downstream; the golden harness stands in for that).
3. Runs `reducer.ExtractCodeCallRows` and tallies each row's `resolution_method`.
4. Compares the per-language tally to `testdata/resolution_tiers.golden.json`.

It runs in the normal `go test ./...` matrix, so the parity gate fires on any
parser or edge-writer change without extra CI wiring.

## Updating the golden

The golden is a regression snapshot of deterministic pipeline output, not a
hand-authored target. When a parser or resolver change *intentionally* shifts a
tier:

1. Confirm the shift is correct and explain it in the PR.
2. Regenerate:

   ```bash
   cd go && ESHU_UPDATE_RESOLUTION_GOLDENS=1 \
     go test ./internal/resolutionparity -run TestResolutionTierGoldens -count=1
   ```

3. Review the `testdata/resolution_tiers.golden.json` diff — an unexpected tier
   collapsing into `repo_unique_name` (the weak global fallback) or
   `unspecified` is a resolution regression, not an update to rubber-stamp.

## Notes

- The fixtures carry no SCIP index and no repository import map, so the `scip`
  and `import_binding` tiers do not appear here; those tiers are covered by the
  reducer's focused fixtures in `internal/reducer`. This package guards the
  heuristic-resolver tier *distribution* against drift.
- Any emitted method outside the ADR #2222 vocabulary fails the test
  immediately, guarding the closed-set invariant per language.
