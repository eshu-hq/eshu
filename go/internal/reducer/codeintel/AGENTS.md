# Agent instructions: internal/reducer/codeintel

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The code-reachability projection (`code_reachability_projection*.go`) and the
#5376/#5494/#5500 code-root verdict builder (`code_root_verdicts*.go`) that
gates it. They moved here together (#6061) because they depend on each other
in both directions — a trial move of either file alone produced an import
cycle. Treat the two source groups as one unit; do not try to split them again
without re-proving the cycle is gone.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/codeintel/README.md`
- `docs/internal/design/package-restructure.md`
- `../evidence-5376-code-root-verdicts.md`, `../evidence-5494-route-liveness.md`,
  `../evidence-5500-lexical-scope-restriction.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: root imports it for `Service.CodeReachabilityProjectionRunner`
  and the exported types, never the reverse. Importing the root back recreates
  the cycle #6061 removed.
- **Reachability rows and verdict rows are replaced atomically, together.**
  `CodeReachabilityRowWriter.ReplaceRepositoryRows` takes both slices and the
  caller (the runner) always builds them from the same
  `CodeReachabilityProjectionInput` in the same cycle. Never split them across
  two writes — a downgraded controller root and reachability rows built from a
  stale root set can disagree if they land in separate transactions.
- **Absence means kept, not dead.** No verdict, no reachability row, or a
  truncated/omitted entity must never be read as "proven dead" anywhere this
  package's output is consumed. The dead-code query's fallback to the legacy
  incoming-edge lookup depends on this.
- **Base-class resolution is lexically scoped (#5500).** `onwardHop` restricts
  candidate resolution to the lexical-prefix chain of the referencing class's
  own namespace before falling back to a broad suffix search. Do not widen
  this to a global suffix-only search — that is the exact P1 false positive
  #5376/#5500 fixed.
- **Route-liveness downgrades require a provably exact route surface.** Only
  downgrade on `RouteEvidenceRouted`/`RouteEvidenceUnrouted`. Any unmodeled or
  ambiguous route registration anywhere in the repo
  (`RubyRailsRouteFacts.HasUnmodeledRoutes`) must keep every controller action
  in that repo — this is the false-negative-safer bias #5494 requires, not an
  optimization to relax.

## Common changes

Adding a new dead-code root kind: extend the kind-generic
`code_root_verdicts` shape (`CodeRootKindRubyRailsControllerAction` is
currently the only value) rather than hard-coding a second table or row shape.

Adding a route-evidence outcome: add the constant beside the existing
`RouteEvidence*` values in `code_root_verdicts_routes.go` and extend
`evaluateRouteLiveness`'s decision table — do not special-case it in the
runner.

Extending the reachability walk: `BuildCodeReachabilityRows` and
`BuildCodeReachabilityRowsWithStats` must stay behaviorally identical except
for the stats return; verify both when changing the walk.

## Failure modes to avoid

- Hand-building `RubyClassEntity` values in a new test instead of feeding real
  parser output. `code_root_verdicts_integration_test.go` exists specifically
  because a hand-built fixture hid the original #5376 P1 (a `QualifiedName`
  shape the real parser's `constantName` could never emit). Prefer extending
  `parseRubyCorpus` over adding another synthetic-fixture test.
- Adding a decode or lookup that silently treats "no data" as "confirmed dead."
  Every ambiguous or missing-evidence branch in this package must resolve to
  keep, not downgrade.
- Renaming or moving `code_reachability_projection*.go` /
  `code_root_verdicts*.go` files without checking
  `docs/public/observability/telemetry-coverage.md` — the two rows for this
  package's `No-Observability-Change` markers cite these basenames by path.

## Do not change without ADR review

- The (scope, generation, repository) partitioning scheme in
  `CodeReachabilityProjectionRunner` — it is the proof that partitions are a
  disjoint conflict domain safe to run concurrently.
- The atomic co-replacement of reachability and verdict rows in one
  transaction.
