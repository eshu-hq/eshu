# Agent instructions: ask/render

## Read first

Before editing any file in this package, read in this order:

1. `doc.go` — package-level godoc contract and one-line description.
2. `render.go` — `Format` constants, `KnownFormat`, `Artifact`, `Valid`, `Validate`.
3. `detect.go` — `DetectFormat` and its cue-based inference logic.
4. `validate.go` — all unexported per-format validators (`validateJSON`, `validateYAML`,
   `validateCSV`, `validateMarkdown`, `validateMermaid`) and `maxArtifactBytes`.

## Invariants — never violate these

- **Pure**: no function in this package mutates the `content` argument.
  `Artifact.Content` must equal the original input string in all code paths.
- **No fabrication**: validators report what is wrong; they do not fix it.
- **Bounded**: every validator checks `oversize(content)` before parsing.
  The cap is `maxArtifactBytes` (1 MiB) in `validate.go`.
- **Panic-safe**: no validator may panic on any input within the size cap.
- **Validate never errors**: `Validate` returns `Artifact`; it has no error return.
  Validation failures are data in `Artifact.Issues`, not Go errors.
- **FormatAuto must be resolved before Validate**: `FormatAuto` is a sentinel value.
  Callers must call `DetectFormat` to resolve it to a concrete format before
  calling `Validate`. Passing `FormatAuto` or any unknown format to `Validate`
  returns `Artifact.Issues = []string{"unresolved format"}`.

## How to add a new format

1. Add a `Format` constant to `render.go` and a matching case in `KnownFormat`.
2. Add a `validateX` function in `validate.go`. Start with the `oversize` check,
   then empty-content check, then format-specific parsing. Return `[]string` of
   human-readable issues; return `nil` on success.
3. Add a `case FormatX:` branch in the `Validate` switch in `render.go`.
4. Add cue-based inference to `DetectFormat` in `detect.go` (new `strings.Contains`
   branch in documented precedence order; update the doc comment).
5. Write tests in `validate_test.go` (for the validator) and `render_test.go`
   (for the `Validate` dispatcher case and `DetectFormat` cue).

## Anti-patterns

- Do not import full external parsers (e.g. a complete Mermaid runtime, an XML
  DOM parser) into this package. Validators are lightweight lints, not round-trip
  parse-and-regenerate pipelines.
- Do not auto-fix or mutate content. If content is invalid, report the issue and
  return the original.
- Do not add bracket-balance checking to the Mermaid validator. Valid Mermaid
  relationship tokens (e.g. ER cardinality `||--o{`) contain unmatched delimiters
  by design; a balance check produces false positives on correct diagrams.
- Do not introduce a Go `error` return on `Validate`. Issues are data.
- Do not call `Validate` with `FormatAuto`; resolve it with `DetectFormat` first.
