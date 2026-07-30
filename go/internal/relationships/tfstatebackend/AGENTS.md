# AGENTS — tfstatebackend

Guidance for LLM assistants editing this package.

## Read first (in order)

1. `doc.go` — package contract.
2. `resolver.go` — the single exported entry point.
3. `../../scope/tfstate.go:33-40` — state-snapshot scope construction
   (where the composite key originates).
4. `../../projector/canonical.go:172` — `TerraformBackend` canonical
   row (where the config-side composite key is recorded).
5. `../../projector/tfstate_canonical.go:14-75` — state-side row
   shapes (Address, Lineage, Serial, BackendKind, LocatorHash).
6. `docs/public/reference/relationship-mapping.md` — current relationship and
   drift-correlation contract.

## Invariants

- `Resolver.ResolveConfigCommitForBackend` MUST return at most one
  `CommitAnchor`. Multi-owner conflicts return
  `ErrAmbiguousBackendOwner`; missing owners return
  `ErrNoConfigRepoOwnsBackend` (`resolver.go:80, 87`).
- "Latest" selection is deterministic: highest `CommitObservedAt`,
  tie-broken by `CommitID` lexicographic ascending. Never randomize
  (`resolver.go:142`).
- The resolver is read-only. It does not mutate canonical rows, queue
  state, or facts.
- A nil `TerraformBackendQuery` is a permitted "no owner" mode that
  always returns `ErrNoConfigRepoOwnsBackend` (`resolver.go:118`).
  Callers wire a real implementation when the storage adapter is ready.

## Common changes scoped by file

- Add a new error class: declare in `resolver.go` near
  `ErrNoConfigRepoOwnsBackend` and `ErrAmbiguousBackendOwner`,
  document the operator-visible failure mode in `doc.go`, and add a
  classifier test in the drift package fixture corpus.
- Change the selection rule: requires architecture-owner approval, updated
  relationship-mapping docs, and regression coverage; tie-break semantics are
  part of the design contract.
- Attach prior-generation evidence: extend the return shape
  (`CommitAnchor` or a sibling helper), keep the resolver call
  bounded — never join across more than one prior generation per
  resolve call.

## Anti-patterns specific to this package

- Do NOT call this resolver in a hot loop without a cache. Every call
  touches the canonical row table.
- Do NOT swallow `ErrAmbiguousBackendOwner` to "try the next repo."
  The conflict is operator-actionable; suppressing it hides graph
  truth.
- Do NOT pick winners by repo name, modification time of files in the
  repo, or any heuristic outside `(CommitObservedAt, CommitID)`.

## What NOT to change without architecture-owner approval

- The single-owner policy. Multi-owner resolution requires explicit
  design and operator documentation.
- The deterministic selection rule.
- The two typed errors. The drift handler depends on
  distinguishing them.

## Related packages

- `tfstatebackend/canonicalwriter` (`./canonicalwriter`) adapts
  `Resolver.ResolveConfigCommitForBackend` for the canonical writer's
  `MATCHES_STATE` edge scoping (#5623), classifying a result into
  `projector.TerraformStateOwnershipOutcome` instead of this package's own
  bare `(string, bool)`-shaped errors. It lives in a separate package
  because `internal/projector` (owner of that outcome type) already
  transitively imports this package, so this package cannot import
  `projector` back without a cycle. If you change `ErrNoConfigRepoOwnsBackend`,
  `ErrAmbiguousBackendOwner`, or `ResolveConfigCommitForBackend`'s return
  contract, check `canonicalwriter.ResolveOwningRepoIDOutcome` for a matching
  update — it is the only place those two errors are classified for the
  canonical writer's scoped-token authorization path, and per its own
  "What NOT to change" section, must not collapse `ErrAmbiguousBackendOwner`
  back into a generic failure.
