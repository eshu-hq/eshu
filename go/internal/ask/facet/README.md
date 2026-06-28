# facet

## Purpose

Deterministically detect **source-tool / language scope intent** in an Ask Eshu
question (#4006, epic #3997), so the answer engine can steer the agent toward the
right `source_tool` / `language` filters and the response can honestly state the
detected scope. It does **not** run any filter.

## Ownership boundary

Owns only the question-text → `Facets` detection. It does **not** execute graph
reads, call MCP tools, or apply filters — the actual scoping runs server-side in
the MCP tool handlers (`list_relationship_edges`, `search_semantic_context`). It
classifies against the canonical source-tool vocabulary in
`go/internal/sourcetool` and the parser language set; it does not own those
vocabularies.

## Exported surface

- `Facets{ SourceTool, Language, UnknownToolMention string }` — the detected
  scope. Empty fields mean "no detected facet".
- `DetectFacets(question string) Facets` — pure, deterministic detection.

## Dependencies

`go/internal/sourcetool` (canonical source_tool vocabulary) and
`go/internal/parser` (recognized languages). Standard library otherwise. No I/O,
no graph/MCP calls.

## Telemetry

None — pure in-process text analysis.

## Gotchas / invariants

- **Honest, never fabricated.** A `source_tool` is reported only when canonical
  (`sourcetool.IsValid`); a non-canonical tool-like word becomes
  `UnknownToolMention` (the answer says "not a recognized tool"), never a guessed
  token.
- **Collision-prone words need a qualifier.** Tokens that are also common English
  words (`go`, `salt`, `chef`, `cargo`, `pip`, `npm`, `maven`) only resolve when a
  disambiguating qualifier is present (e.g. "salt formula", "deploy via cargo",
  "go module/repo"), so "pinch of salt" or "where should I go" do not false-fire.
- **Detected intent, not applied filter.** The result reflects what the user
  asked, not what the agent did; callers must frame it as detected intent and
  point to the query trace for the filters actually applied.

## Related docs

- [Edge Source-Tool Provenance](../../../../docs/public/reference/edge-source-tool-provenance.md)
- `go/internal/sourcetool` — the canonical source_tool vocabulary.
