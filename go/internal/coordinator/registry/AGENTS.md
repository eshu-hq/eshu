# Coordinator package-registry namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for coordinator scheduling and runtime invariants.
3. `package/AGENTS.md`, `package/README.md`, and the leaf source before
   changing a package-registry planning contract.

## Invariants

- Keep this package documentation-only.
- Keep planner implementation in the `package` leaf and runtime behavior in
  the coordinator root.
- The `package` leaf must not import the coordinator root.
- Scheduling, clocks, gates, durable admission, retries, queues, leases, and
  telemetry remain in the coordinator root.
- The leaf directory is named for the `package` keyword; its Go package name
  is `packages`. Always import it with that alias.

## Common changes

Adding or changing a supported ecosystem touches
`package/ecosystem_targets.go`, which is the source of truth. One document
states the count, `package/README.md`; update it in the same change. No parent
README carries an ecosystem count, so do not go looking for one.

## Verification

Run the `package` leaf tests, every direct consumer, the package-documentation
gate, dirgate, and the moved-file-reference guard.
