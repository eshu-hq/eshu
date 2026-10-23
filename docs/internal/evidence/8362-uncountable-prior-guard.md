# #8362: ingestion commit stores over a delta prior with no base full generation

## Problem

The destructive-successor guard (`#7926`) counts the scope's prior generation
on the base connection before the commit transaction opens
(`successorPriorForGuard` in
`go/internal/storage/postgres/ingestion_backfill_seam.go`). The count reuses
`FactStore.CountFacts`, which resolves the delta-base union and refuses with
`projector.DeltaBaseMissingError` when the counted generation is a delta whose
base full generation is missing (never projected, or retention-pruned).

When the scope's active generation is such a delta, every later commit for the
scope fails closed in the prior-fact count — including the forced full re-parse
that restores the base. The collector classifies the error as fatal
(`failure_class=composite_runner_fatal`, `retryable=false`), so the composite
runner drains its peers, the reindex watermark is never satisfied, and each
retry re-parses the whole repository before failing identically:

```text
commit scope generation: count prior generation facts for scope <scope>: delta base full generation is missing for scope <scope> generation <id>
```

The named generation is the scope's active generation.

## Fix

`successorPriorForGuard` handles a missing-base count failure by falling back
to the candidate generation's own `fact_records` count
(`successorPrior.orphanFallback`) instead of failing the commit. Every other
count failure, and a fallback failure, still fails closed.
`refuseDestructiveSuccessor` in
`go/internal/storage/postgres/generation_freshness.go` then runs the normal
`#7926` predicate against that fallback count, and still refuses an empty
successor over a found orphan prior even when the fallback count is zero: a
sharply shrunken successor is refused, the repair (the forced full re-parse)
still stores, and an empty successor retracts everything regardless of the
prior's size. The trusted-deletion escape hatch is unchanged.

The forced sync path already snapshots full; the commit was the layer blocking
the repair, not the snapshot kind. The projector-side missing-base refusals
(delta-baseline fence, delta-base union) are unchanged by design: they still
mark the generation superseded and record a reindex watermark.

## Proof

No-Regression Evidence (#8362): five hermetic fixture tests in
`go/internal/storage/postgres/ingestion_successor_uncountable_prior_test.go`
run against the store fakes, plus the five pre-existing `#7926` backstop
tests. Before the hardening, a sharply shrunken successor over an orphan
prior stored and a fallback failure was ignored; after it both fail closed
as refused or errored, and all ten pass:

```text
go test ./internal/storage/postgres/ -run 'TestCommitStoresForcedFullOverMissingBasePrior|TestCommitRefusesEmptySuccessorOverMissingBasePrior|TestCommitRefusesSharplyShrunkenSuccessorOverMissingBasePrior|TestCommitFailsClosedOnFallbackCountError|TestCommitFailsClosedOnPriorCountError|TestCommitRefusesEmptySuccessorOverContentBearingPrior|TestCommitRefusesSharplyShrunkenSuccessor|TestCommitStoresTrustedEmptySuccessor|TestCommitStoresEmptySuccessorOverEmptyPrior|TestCommitStoresMildlyShrunkenSuccessor' -count=1
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres
```

The change adds one bounded read only on the missing-base branch: the
candidate's own `fact_records` count (`countFactsQuery`, one
scope/generation). The `errors.As` branch runs only when `CountFacts`
already failed, so success paths, locks, and workers are unchanged and no
plan or benchmark evidence applies.

Observability Evidence (#8362): the fallback records a
`successor_prior_orphan_fallback` ingestion commit stage
(`candidate_generation_id` and `prior_fact_count_fallback` attached), and
the refusal log carries `prior_orphan_fallback=true` alongside
`prior_fact_count`, so an operator can tell a fallback-sized prior from a
union-sized one.

Later sizing change: #8750 made the guard compare collector rows only and size git deltas by their file union; see `docs/internal/evidence/8750-successor-guard-sizing.md`.
