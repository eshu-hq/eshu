# AGENTS.md — internal/environment guidance for LLM assistants

## Read first

1. `go/internal/environment/environment.go` — the full contract surface.
2. `docs/public/reference/environment-alias-contract.md` — the normative contract doc.

## Invariants

- **Normalize is trim + lowercase** — no other normalization exists.
- **Canonical maps only three aliases** — production→prod, staging→stage,
  development→dev. All other known canonical names pass through as-is.
- **Unknown values are never rejected or invented** — they pass through
  normalized.
- **IsKnownToken is the 12-token union** — exactly {prod, production, qa,
  stage, staging, uat, preprod, dev, development, test, sandbox, preview}.
  It is case-sensitive; callers normalize first.
- **EvidenceClass is a closed vocabulary** — 10 classes: 7 existing producers
  plus 3 defined for later wiring. ParseEvidenceClass rejects unknown values.
- **State is a closed vocabulary** — StateBound ("bound") and
  StateEnvironmentUnbound ("environment-unbound"). This PR defines them;
  existing 'unknown' buckets and missing_environment tallies are not rewired.

## Common changes

- **Adding an alias** — add to `aliasToCanonical` in `environment.go`, update
  the contract doc, and run the full test suite. An alias addition may change
  consumer output if a previously-unknown value now maps to a canonical name.
- **Adding a known token** — add to `knownTokens`, update the count assertion
  in `TestIsKnownToken`, update the contract doc.

## Failure modes

- **Exact-match graph joins break on case mismatch** — the USES edge uses
  exact string comparison in Cypher. Canonical normalization in the reducer
  and query layer fixes this for consumer-side comparisons, but the graph
  join (go/internal/storage/cypher/workload_cloud_relationship_writer.go:22,25)
  remains a documented follow-on.

## Anti-patterns

- **Adding environment-specific logic here** — this package is a pure data
  contract. Do not add config reading, file I/O, network calls, or
  environment admission logic.
- **Inventing environments** — never map an unknown value to a canonical
  name without explicit evidence. The no-invention rule is absolute.

## What NOT to change without an ADR

- The alias table entries (production→prod, staging→stage, development→dev).
- The known-token count (12).
- The StateClosed vocabulary values.
