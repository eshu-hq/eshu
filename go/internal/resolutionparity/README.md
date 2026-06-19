# resolutionparity

Per-language call-resolution accuracy goldens and the CI parity gate for
code-edge resolution provenance (issue #2226, part of epic #2218), plus the
source-derived caller-to-callee correctness gate for issue #2708.

## What it does

Code-call edges carry a `resolution_method` from the closed
[ADR #2222](../../../docs/internal/design/2222-resolution-provenance-code-edges.md)
vocabulary (emitted by the reducer in #2223). This package measures, per
language, how the resolver distributes calls across those tiers and pins the
distribution as a golden so a parser or resolver regression that shifts the
tiers fails CI.

`TestResolutionTierGoldens`:

1. Parses every matching file in a language's `tests/fixtures/sample_projects`
   corpus with the real `parser.DefaultEngine()`. Languages without a richer
   sample-project tier corpus reuse source-authored exact-edge fixtures as
   same-file smoke snapshots so every registered call-graph source language has
   at least one tier entry.
2. Injects deterministic synthetic uids onto parsed entities (the ingester
   assigns real uids downstream; the golden harness stands in for that).
3. Runs `reducer.ExtractCodeCallRows` and tallies each row's `resolution_method`.
4. Compares the per-language tally to `testdata/resolution_tiers.golden.json`.

It runs in the normal `go test ./...` matrix, so the parity gate fires on any
parser or edge-writer change without extra CI wiring.

`TestGoldenCallGraphCorrectnessHarness` adds a second gate. It parses
source-authored fixtures, injects deterministic entity uids, runs
`reducer.ExtractCodeCallRows`, and compares the observed caller→callee edges
with independent expected edges through `parser/goldenaudit`. This catches a
wrong target even when the edge keeps the same `resolution_method` tier.

The exact-edge gate currently has source-backed passing fixtures for C, C#,
C++, Dart, Elixir, Go, Groovy, Haskell, Java, JavaScript, Kotlin, Perl, PHP,
Python, Ruby, Rust, Scala, Swift, TSX, and TypeScript. It also includes a
Python `import_binding` fixture and a SCIP-shaped fixture that exercises the
reducer's `function_calls_scip` path without requiring external `scip-*`
binaries in CI.

`sourceCallGraphFixtureGaps` is intentionally empty when all registered
call-graph source languages have exact-edge fixtures. The coverage test fails
when a source language is neither covered by a fixture nor listed as an explicit
gap, and the shadowing test fails if a passing fixture remains in the gap map.
Dart and Swift currently use source-backed type-entity caller fixtures because
their parsers still emit declaration-line spans for function bodies.

The source-backed entries in `resolution_tiers.golden.json` are deliberately
limited smoke coverage. They keep long-tail languages from disappearing from the
tier gate, but they do not prove non-`same_file` behavior such as import binding,
SCIP, or cross-repo resolution. Broader tier distribution remains owned by the
sample-project corpora and future language-specific fixtures.

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

- `TestResolutionTierGoldens` uses richer sample-project corpora where
  available and source-authored same-file smoke fixtures for the remaining
  call-graph languages. The sample corpora guard heuristic-resolver tier
  *distribution* against drift; the smoke fixtures keep long-tail languages
  visible until they earn broader corpora.
- `TestGoldenCallGraphCorrectnessHarness` uses source-authored truth and guards
  target correctness. Do not generate its expected caller→callee edges from
  Eshu's own output.
- Any emitted method outside the ADR #2222 vocabulary fails the test
  immediately, guarding the closed-set invariant per language.
