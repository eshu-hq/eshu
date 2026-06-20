# ask/render

Format detection and validation for Ask Eshu output artifacts.

## What this package does

The `render` package provides two operations on LLM-produced output:

1. **Format detection** (`DetectFormat`) — infers the intended output format from an
   explicit format request string and the user question. Supported formats:
   `markdown`, `mermaid`, `json`, `yaml`, `csv`. The `auto` value is a sentinel
   that DetectFormat resolves; it is never a valid output format itself.

2. **Validation** (`Validate`) — dispatches content to a format-specific validator and
   returns an `Artifact` containing the original content and any issues found.
   Supported formats: `markdown`, `mermaid`, `json`, `yaml`, `csv`.

## Typical flow

```
format := render.DetectFormat(question, requestedFormatString)
artifact := render.Validate(format, llmOutput)
if !artifact.Valid() {
    // artifact.Issues lists what is wrong
}
```

Callers must resolve `FormatAuto` via `DetectFormat` before calling `Validate`.
Passing `FormatAuto` or an unknown format to `Validate` returns an artifact with
the issue `"unresolved format"`.

## Per-format validator notes

| Format   | What is checked                                                       |
|----------|-----------------------------------------------------------------------|
| JSON     | Parses as valid JSON via `encoding/json`                              |
| YAML     | Parses as valid YAML via `gopkg.in/yaml.v3`                           |
| CSV      | Parses with consistent column counts via `encoding/csv`               |
| Markdown | Non-empty and within the size cap (no structural grammar to enforce)  |
| Mermaid  | First non-empty line starts with a recognized diagram-type keyword    |

### Mermaid caveat

The Mermaid validator is a bounded syntactic lint, not a full Mermaid parse. It
checks only that the first token on the first non-empty line is a recognized
diagram-type keyword (e.g. `graph`, `flowchart`, `sequenceDiagram`). Bracket
balance is intentionally not checked: valid Mermaid relationship tokens (e.g.
ER cardinality notation `||--o{`) contain unmatched delimiters by design, so a
balance check produces false positives on correct diagrams. A diagram that
passes this validator may still be rejected by a Mermaid renderer.

## Size cap

All validators enforce a 1 MiB (`1 << 20` bytes) size cap. Content exceeding
this limit is rejected with the issue `"artifact exceeds size limit"` without
further parsing.

## Invariants

- **Pure**: the package never mutates content. `Artifact.Content` always equals the
  input string, regardless of validation outcome.
- **No fabrication**: validators never invent or repair content.
- **Issues are data**: `Validate` never returns an error. Validation problems are
  reported as strings in `Artifact.Issues`. An empty or nil `Issues` slice means
  the content is valid (`Artifact.Valid()` returns true).
- **Panic-safe**: no validator panics on any input within the size cap.
