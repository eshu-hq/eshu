# AGENTS.md — internal/sourcetool guidance for LLM assistants

## Read first

1. `go/internal/sourcetool/README.md` — purpose, exported surface, invariants
2. `go/internal/sourcetool/sourcetool.go` — `Canonical` var and `IsValid()`
3. `docs/public/reference/edge-source-tool-provenance.md` — the vocabulary
   definition this package mirrors

## Invariants this package enforces

- **`Canonical` is the closed enum** — every consumer validates against it via
  `IsValid`. Do not add ad-hoc string comparisons elsewhere.
- **No duplicates in `Canonical`** — the test `TestCanonicalHasNoDuplicates`
  catches this at CI time. Duplicates silently break the `validTokens` map.
- **`"unknown"` is always present** — it is the explicit fallback for edges
  whose tool is not provable; its absence would break the fallback contract.

## Common changes

- **Add a new tool** — add the lowercase token to `Canonical` (in vocabulary
  order), update `docs/public/reference/edge-source-tool-provenance.md`, and
  extend the edge-source-tool drift gate (`#4002`). Tests check for duplicates
  automatically.

## Anti-patterns

- **Copying `Canonical` into another package** — always import and reference
  `sourcetool.Canonical` or `sourcetool.IsValid`. A local copy will drift.
- **Case-insensitive matching inside `IsValid`** — the contract is exact match;
  callers lowercase/trim. Do not add a case fold inside `IsValid`.
