# Environment Alias Contract

## Purpose

Centralizes environment naming across the Eshu platform. Before this package,
environment tokens, aliases, and normalization rules were duplicated across
three reducer/query locations with inconsistent case handling.

## Ownership boundary

Owns the canonical environment-alias table, the single normalization rule,
the known-token set for artifact-path detection, and the closed evidence-class
and state vocabularies. Does not own fact emission, environment admission, or
the USES edge exact-join (documented follow-on).

## Exported surface

- `Normalize(raw string) string` — trim + lowercase, the platform normalization rule.
- `Canonical(raw string) string` — normalize + alias-map; unknown values pass through normalized.
- `IsKnownToken(token string) bool` — 12-token union for artifact-path detection.
- `EvidenceClass` — closed typed string for evidence provenance classes.
- `State` — closed typed string for environment binding states.

See `doc.go` for the full godoc contract.

## Dependencies

No internal Eshu dependencies. Standard library only (`fmt`, `strings`).

## Telemetry

No-Observability-Change: pure string functions with no new I/O. All consumers
surface their environment results through existing pipeline signals
(reducer execution counters, query handler spans, HTTP route attribution).

## Gotchas / invariants

- Unknown values pass through normalized — never rejected, never invented.
- The alias table maps only production→prod, staging→stage, development→dev.
- `IsKnownToken` is case-sensitive (callers must normalize first).
- The package is a pure data contract; it does not read config, files, or
  the network.

## Related docs

`docs/public/reference/environment-alias-contract.md` — the full contract doc.
