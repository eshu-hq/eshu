# AGENTS.md - internal/coordinator/schedule guidance

## Read first

1. `go/internal/coordinator/schedule/README.md` for this package's ownership
   boundary and invariants.
2. `go/internal/coordinator/schedule/target.go` for target-class ranking and
   Postgres-precision-safe ordinal spacing.
3. `go/internal/coordinator/schedule/derivation.go` for planning-mode
   constants, rotation offsets, plan-key rendering, and owned-dependency
   version parsing.
4. `go/internal/coordinator/schedule/derived_target_budget.go` for skip-reason
   constants and skip-evidence rendering.
5. `go/internal/coordinator/vulnerability/intelligence_scheduler.go` and
   `go/internal/coordinator/registry/package/scheduler.go` for the two callers
   that build a scheduler on top of this package.

## Invariants

- Keep this package free of any import on `internal/coordinator` (the root
  package) or on either family package that calls it.
- Keep `TargetClassRank` ranking an unrecognized class last so it never
  preempts a known class.
- Keep `DerivedTargetRotationOffset`'s truncation basis identical to whatever
  basis `DerivedTargetPlanKey` uses for the same interval, so a rotating
  instance never pages under a plan key from a different bucket.
- Keep `DerivedTargetReadLimit`'s lookahead at exactly one row; changing it
  changes every caller's "budget exhausted" detection.

## Common changes

- A new target class needs a new constant plus a `TargetClassRank` case;
  callers that switch on target class may also need updating.
- A new skip reason needs a new constant in
  `derived_target_budget.go` and, if it is caller-specific, a
  `DerivedTargetSkipEvidenceByReasonForClass` call site update rather than a
  change to the shared renderer.
- Rotation or plan-key format changes affect every scheduler that pages a
  derived target set; add or update the focused test in this package before
  touching a caller.

## Failure modes

- A negative or zero `ordinal` to `TargetCreatedAt` clamps to zero rather than
  producing a time before `observedAt`.
- An unparseable dependency version (`latest`, a range, a workspace or VCS
  reference) makes `ExactOwnedDependencyVersion` return `("", false)` instead
  of an error; callers must check the boolean.
- A blank reason or ecosystem passed to `RecordDerivedTargetSkip` is silently
  dropped rather than bucketed under an empty key.

## Verification

Run this package's focused tests, then the recursive coordinator tree
(`internal/coordinator/...`) since `vulnerability` and `registry/package` both
call these functions.
