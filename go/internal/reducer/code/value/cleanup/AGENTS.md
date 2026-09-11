# Agent instructions: internal/reducer/code/value/cleanup

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Removes reducer-owned value-flow evidence from older generations beside the
normal reducer intent loop (issue #6061). Moved out of the reducer root as
its own package. See the README's Purpose and Ownership boundary sections for
what this package owns and reuses from `code/taint` and `sharedintent`.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/value/cleanup/README.md`
- `docs/internal/design/reducer-target-tree.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the value-flow stanza of
  `compat_projection.go` and `Service.CodeValueFlowStaleCleanupRunner`'s
  field type), never the reverse.
- **The ledger-driven path and the plain retractor path are mutually
  exclusive per evidence family, checked in `validate`.** Wiring neither
  `TaintEvidence` nor both of `TaintLedger`+`TaintWriter` (same for interproc)
  is a validation error, not a silent no-op.
- **`leaseDomain` is this runner's own single-partition lease identity.**
  Do not reuse `"code_value_flow_stale_cleanup"` for another side runner's
  lease claim.

## Common changes

Adding a new evidence family to the cleanup sweep: add its retractor port
(mirroring `CodeTaintStaleEvidenceRetractor`), a ledger-driven field pair on
`Runner` if it needs the anchored by-UIDs path, and thread both through
`RunOnce`'s per-candidate loop alongside the existing taint/interproc sweeps.

## Failure modes to avoid

- Exporting a new unexported helper "just in case" a future caller needs it.
  `Runner`, `RunnerConfig`, `Result`, `CurrentGeneration`,
  `CurrentGenerationReader`, `CodeTaintStaleEvidenceRetractor`,
  `CodeInterprocStaleEvidenceRetractor`, and `ErrCurrentGenerationsRequired`
  are exported because the reducer root's compat forwarders and external
  wiring need them; nothing else in this package has an external caller
  today.
- Adding the ledger-driven fields without keeping the plain retractor path
  working — `validate` and `RunOnce` both branch on "is the ledger pair
  wired", so a caller that wires only the plain retractor must keep working.

## Do not change without ADR review

- The lease identity constants (`leaseDomain`, `leasePartitionID`,
  `leasePartitionCount` in `runner.go`) — changing the domain string changes
  which lease row this side runner claims.
