# Coordinator governance namespace agent guide

## Read first

1. `README.md` for this documentation-only namespace boundary.
2. `../AGENTS.md` for coordinator scheduling and runtime invariants.
3. `audit/AGENTS.md`, `audit/README.md`, and the leaf source before changing
   an audit contract.

## Invariants

- Keep this package documentation-only.
- Keep event construction in the `audit` leaf and appender wiring in the
  coordinator root or the consuming package.
- The `audit` leaf must not import the coordinator root.
- Do not add a shared appender interface here. Consumers declare their own.
- Audit identity is durable. Treat any change to `audit.Hash` or
  `audit.CorrelationID`, or to the prefix and argument order at a call site,
  as a contract change and re-check all call sites.

## Common changes

Adding a direct `.go` file here changes dirgate's view of the root: the root
`governance_audit.go` then matches this directory name and needs its
`//nolint:dirgate` marker. Run `bash scripts/verify-dirgate.sh --all` after
any structural change in this directory.

## Verification

Run the `audit` leaf tests, every direct consumer, the package-documentation
gate, dirgate, and the moved-file-reference guard.
