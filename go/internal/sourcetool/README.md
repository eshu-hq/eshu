# sourcetool

## Purpose

`sourcetool` is the single source of truth for the canonical `source_tool`
vocabulary used by read-side API, MCP, and console consumers in epic
[#3997](https://github.com/eshu-hq/eshu/issues/3997).

It exports the ordered `Canonical` token list and an `IsValid` predicate so
every consumer validates against the same closed enum.

## Where this fits

```
docs/public/reference/edge-source-tool-provenance.md  ← vocabulary definition
go/internal/sourcetool  ← code mirror of that vocabulary
go/internal/query       ← API read surface (uses IsValid for filter params)
```

## Exported surface

- `Canonical []string` — ordered set of all valid `source_tool` tokens.
- `IsValid(token string) bool` — true when `token` is a member of `Canonical`.
  Performs an exact match; callers must lowercase and trim first.

## Invariants

- `Canonical` must never have duplicates.
- `"unknown"` must always be present as the explicit fallback token.
- New tokens are added here and the doc updated in the same PR.

## Dependencies

Standard library only. No internal packages.

## Related docs

- [Edge Source-Tool Provenance](../../../docs/public/reference/edge-source-tool-provenance.md)
