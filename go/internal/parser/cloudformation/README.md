# CloudFormation Parser

## Purpose

`internal/parser/cloudformation` owns CloudFormation and SAM template evidence
shared by JSON and YAML parser adapters. It recognizes bounded template shapes,
evaluates simple condition expressions, and extracts resource, parameter,
output, condition, import, and export rows.

## Ownership boundary

This package is responsible for CloudFormation document classification,
condition evaluation, and payload row construction. JSON and YAML adapters own
file decoding, top-level parser dispatch, and attaching these rows to their
language payloads.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Result` groups the extracted CloudFormation buckets.
- `IsTemplate` reports whether a decoded document is CloudFormation or SAM.
- `Parse` extracts deterministic bucket rows for one decoded document,
  stamping every entity with the single document-root line.
- `ParseWithPositions` extracts the same buckets but stamps each entity with
  its own real line_number/end_line from a caller-measured `Positions` value
  when the caller has one (issue #5328). `Parse` is `ParseWithPositions`
  called with a zero `Positions`.
- `Positions`, `SectionPositions`, and `EntityPosition` carry the per-entity
  line evidence a caller with a real source-position walk (currently only the
  YAML adapter) passes to `ParseWithPositions`.

## Dependencies

This package imports `internal/parser/shared` for deterministic bucket sorting
and the Go standard library. It must not import the parent parser package,
collector, graph storage, query, or reducer packages.

## Engine test surface

`engine_yaml_cloudformation_lines_test.go` (external package
`cloudformation_test`, relocated from the parent by #6062) drives
`parser.DefaultEngine().ParsePath` over YAML and JSON templates — the
`tests/fixtures/ecosystems/cloudformation_comprehensive` corpus plus inline
merge-key, multi-document, and nested-stack fixtures — to prove real
per-entity `line_number`/`end_line` truth: 6 test functions, from
`rg --no-filename -o '^func Test' engine_yaml_cloudformation_lines_test.go | wc -l`
in this directory. It may import `internal/parser` because it sits in the
external `cloudformation_test` package: an external test package is compiled
separately from the package under test, so the import does not close the cycle
that `internal/parser` (and `parsertest`, which imports it) depending on this
package would otherwise create. Keep that exception limited to black-box tests
of the public parent engine; the in-package test files stay white-box and must
not import the parent. Fixture writes come from `parsertest.WriteFile`, and
`cfnFixtureDir` (in the same file) resolves the fixture corpus via
`runtime.Caller` — the parent's `repoFixturePath` is unexported and declared in
`internal/parser`'s own `testhelpers_test.go`, and test files are not
importable across packages, so `cloudformation_test` could not call it from
any location.

Run the engine tests from the `go/` module root with
`go test ./internal/parser/cloudformation -count=1`; pin the engine subset with
`../scripts/go-test-run-guard.sh 6 'TestDefaultEngineParsePath(YAML|JSON)CloudFormation' -- ./internal/parser/cloudformation -count=1`,
which runs from the `go/` module root and fails closed if fewer than 6 tests
match (minimum derived from
`go test -list 'TestDefaultEngineParsePath(YAML|JSON)CloudFormation' ./internal/parser/cloudformation`).

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path that calls the parent parser engine.

## Gotchas / invariants

`Parse` preserves the legacy bucket names and row fields consumed by JSON and
YAML callers. Keep the output deterministic: map keys are sorted before rows
are emitted, and row slices are sorted by line number then name.

Condition evaluation is intentionally bounded to literal booleans, parameter
defaults, `Condition`, and simple `Fn::Equals`, `Fn::And`, `Fn::Or`, and
`Fn::Not` forms. Dynamic or unresolved values remain unevaluated rather than
inventing deployment truth.

An Export always inherits its owning Output's `EntityPosition` rather than
getting a separately-walked line, because an Export always nests inside its
Output in the template shape. This package emits no metrics, spans, or logs
(see Telemetry above), so a caller that wants to observe how often its own
position walk degrades to a `SectionPositions.FallbackLine` (or omits
`end_line` entirely) must read that signal back out of its own return value
and record it itself; the YAML adapter does this via a
`cloudformation_position_fallbacks` payload row the collector layer turns
into telemetry.

## Related docs

- `docs/public/languages/support-maturity.md`
