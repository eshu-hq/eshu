# Collector document namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for collector runtime invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and source before changing
   a document contract.

## Invariants

- Keep this package documentation-only.
- Keep document implementation in leaf packages and collection behavior in
  the owning collector.
- Leaves must not import the collector root. Depending on the preflight
  manifest gate is the established seam (`export` reads `preflight/manifest`
  decisions only, never provider clients).
- Offline-only posture: no live provider calls, no archive unpacking beyond
  the leaf's reviewed scope, no fact emission outside documentation facts.

## Common changes

- Add a leaf by nesting it here with its own doc trio; keep this namespace
  free of declarations.
- Changing a leaf contract starts at that leaf's `AGENTS.md`.
