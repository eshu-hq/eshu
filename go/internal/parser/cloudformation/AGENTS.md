# AGENTS.md - internal/parser/cloudformation

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for JSON and YAML callers.
3. `parser.go` - `Result`, `IsTemplate`, `Parse`, and primary row extraction.
4. `conditions.go` - bounded CloudFormation condition evaluation.
5. `condition_helpers.go` - boolean and comparable helpers.
6. `parser_helpers.go` - output/import helpers and deterministic sorting inputs.
7. Parent callers in `../json_language.go` and `../yaml_language.go`.

## Invariants this package enforces

- Do not import the parent `internal/parser` package. JSON and YAML adapters
  depend on this package, not the other way around.
- Preserve the bucket field names used by parent JSON and YAML payloads.
- Keep row order deterministic by sorting map keys and final row slices.
- Do not treat unresolved CloudFormation expressions as evaluated truth.

## Common changes

- Adding a new bucket field requires focused tests in this package plus parent
  JSON or YAML parser tests that prove the field survives payload attachment.
- Expanding condition support requires positive, negative, and unresolved cases.
- A new intrinsic parser belongs here only when both JSON and YAML callers can
  use the same decoded document shape.

## Failure modes

- Missing CloudFormation rows usually means `IsTemplate` failed to recognize the
  decoded document shape.
- Flaky row order usually means a map iteration path was added without sorting.
- Wrong condition flags usually mean `evaluateConditionValue` started treating a
  dynamic expression as resolved.

## What not to change without an ADR

- Do not make CloudFormation parsing scan the repository or read files from this
  package. Callers supply one decoded document at a time.
- Do not add graph, collector, or reducer dependencies to this package.
