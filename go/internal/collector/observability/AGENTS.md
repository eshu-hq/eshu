# Collector observability namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for collector runtime invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and source before changing
   an observer contract.

## Invariants

- Keep this package documentation-only.
- Keep observer implementation in leaf packages and collection behavior in
  the owning collector.
- The `tempo` leaf retains its existing collector-root dependency; do not
  treat this subtree as independently extractable.
- Provider access, fact emission, ACL handling, security review, and telemetry
  remain in the owning collector slice.

## Common changes

Move or add an observability leaf only with a production-and-test import edge
census. Update all consumers in the same change; do not leave forwarding
packages or duplicate contracts.

## Verification

Run the changed leaf tests, every direct consumer, the package-documentation
gate, dirgate, and the moved-file-reference guard.
