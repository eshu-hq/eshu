# AGENTS.md - internal/parser/gradle/selector guidance

## Read first

1. `README.md` - why the rule exists and who shares it.
2. `doc.go` - godoc contract.
3. `exact.go` - `Exact`, the single exact-version rule.
4. `testdata/versions.txt` - the table every Gradle reader must agree with.

## Invariants this package enforces

- `Exact` fails closed. Unknown or odd text is not exact; a reader then marks
  the row unresolved. Never widen it to accept a selector to "recover" a
  version.
- Imports only the Go standard library. No telemetry, no allocation
  (`TestExactAllocatesNothing`).
- Every Gradle reader that marks a version resolved calls `Exact` and tests
  itself against `testdata/versions.txt`. Do not fork a second rule.

## Common changes and how to scope them

- A new Gradle selector form: add a `not-exact` row to
  `testdata/versions.txt` first (cite the Gradle doc in the commit), watch
  the reader tests fail, then change `exact.go`.
- Table text must hold no whitespace or quote, so reader tests can embed it
  in any notation.
- Keep `go/internal/reducer/payloadcore.ExactManifestDependencyVersion` in
  step: it is the reducer-side backstop and must reject the same selector
  families, without rejecting other ecosystems' exact forms (an interior
  `+` is semver build metadata there).
