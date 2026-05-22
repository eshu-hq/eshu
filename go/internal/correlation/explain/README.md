# Correlation Explain

## Purpose

`correlation/explain` renders one evaluated correlation result as a stable,
line-oriented text block for explain APIs and operator diagnostics.

## Ownership boundary

This package owns only the text format and deterministic sort order. It does not
evaluate rules, apply admission, select winners, mutate candidates, or attach
rejection reasons.

## Exported surface

Use `doc.go` and `go doc ./internal/correlation/explain` for the contract.
`Render` takes an `engine.Result` and returns the header, match counts,
rejection reasons, and evidence lines in stable order.

## Dependencies

`explain` depends on `correlation/engine` for result shape and
`correlation/model` for evidence atoms.

## Telemetry

None. Callers attach telemetry around the explain API or MCP route that invokes
`Render`.

## Gotchas / invariants

- Output ordering is part of the contract: header, sorted match counts, sorted
  rejection reasons, then evidence sorted by ID, source system, and evidence
  type.
- Evidence is cloned before sorting so rendering does not mutate the input
  result.
- Confidence is formatted with two decimal places.
- There is no trailing newline.
- Evidence values are rendered verbatim. Callers that need parser-safe escaping
  must add it outside this package.

## Related docs

- `go/internal/correlation/engine/README.md`
- `go/internal/correlation/model/README.md`
- `go/internal/correlation/README.md`
- `docs/public/reference/relationship-mapping.md`
