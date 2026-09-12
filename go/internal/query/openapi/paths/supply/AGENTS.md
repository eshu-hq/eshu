# supply — agent instructions

This directory is a namespace, not a package with behavior. Do not add
runtime code to `doc.go`; add it to `chain/`, the one child package that
owns it.

## Invariants

- This directory exists only so `supplychain` can nest as `supply/chain`
  instead of staying a glued compound (`docs/internal/naming.md` rule 3).
  Do not add a sibling to `chain/` under this directory unless a future
  supply-chain-adjacent family is deliberately split out of it — a
  genuinely new top-level family belongs directly under `openapi/paths/`,
  not nested here just because the name looks related.
- `chain/` MUST NOT import `openapi` — the parent imports it, and the
  reverse would cycle. See `chain/AGENTS.md`.
