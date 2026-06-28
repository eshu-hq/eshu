# AGENTS.md — go/internal/ask/facet

Scoped agent instructions for the Ask facet-detection package.

## What this package is

Deterministic detection of source-tool / language **scope intent** in an Ask
question. It reports detected intent; it never executes a filter (that happens in
the MCP tool handlers).

## Rules when editing here

- **Truth-first, no fabrication.** Only report a `source_tool` that is in
  `sourcetool.Canonical`. A tool-like word that is not canonical must surface as
  `UnknownToolMention`, never a guessed/normalized tool. When unsure, return
  empty.
- **Keep the canonical vocabulary single-sourced.** Validate source tools against
  `go/internal/sourcetool` (do not hand-maintain a second list). Languages come
  from `go/internal/parser`.
- **Guard collision-prone tokens.** Any token that is also a common English word
  (`go`, `salt`, `chef`, `cargo`, `pip`, `npm`, `maven`, …) must require a
  disambiguating qualifier before it resolves — add adversarial table tests for
  any new such token proving the bare word does NOT fire.
- **Deterministic only.** No LLM, no I/O, no map-iteration-order dependence.
- **Detected intent ≠ applied filter.** Do not let callers claim a filter was
  applied; the query trace is the source of truth for applied filters.

## Tests

`facet_test.go` is table-driven and MUST include adversarial common-word cases
(the bare word yields empty) alongside genuine-mention cases.
