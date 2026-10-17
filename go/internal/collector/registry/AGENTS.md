# Collector registry namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for collector runtime invariants.
3. The destination leaf's `AGENTS.md`, `README.md`, and source before changing
   a registry contract.

## Invariants

- Keep this package documentation-only.
- Keep registry implementation in leaf packages and collection behavior in
  the owning collector.
- ECR belongs to the `oci` leaf, not the `package` leaf.
- Provider access, fact emission, credential handling, and telemetry remain
  in the owning collector slice.

## Common changes

- Move or add a registry leaf only with a production-and-test import edge
  census. Update all consumers in the same change; do not leave forwarding
  packages or duplicate contracts.

## Verification

Run the changed leaf tests, every direct consumer, the package-documentation
gate, dirgate, and the moved-file-reference guard.
