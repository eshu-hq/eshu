# Collector conformance namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for collector runtime invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and source before changing
   a conformance contract.

## Invariants

- Keep this package documentation-only.
- Keep harness implementation in leaf packages and collection behavior in
  the owning collector.
- Leaves must not import the collector root except to drive the exact
  production path under test (`parity` drives `ClaimedService`); no other
  collector-root reads belong here.
- Harnesses stay credential-free: in-memory fixtures only, no live provider
  calls, no graph writes outside the harness readback model.

## Common changes

- Add a leaf by nesting it here with its own doc trio; keep this namespace
  free of declarations.
- Changing a leaf contract starts at that leaf's `AGENTS.md`.
