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

`successorPriorForGuard` degrades a missing-base count failure to an
uncountable prior (`successorPrior.uncountable`) instead of failing the commit.
Every other count failure still fails closed. `refuseDestructiveSuccessor` in
`go/internal/storage/postgres/generation_freshness.go` then keeps only the
emptiness check for an uncountable prior: a non-empty successor stores (the
repair lands and the watermark can converge), while an empty successor is still
refused under the destructive-successor reason, since it retracts everything
regardless of the prior's size. The trusted-deletion escape hatch is unchanged.

The forced sync path already snapshots full; the commit was the layer blocking
the repair, not the snapshot kind. The projector-side missing-base refusals
(delta-baseline fence, delta-base union) are unchanged by design: they still
mark the generation superseded and record a reindex watermark.

## Proof

No-Regression Evidence (#8362): three hermetic fixture tests in
`go/internal/storage/postgres/ingestion_successor_uncountable_prior_test.go`
run against the store fakes, plus the five pre-existing `#7926` backstop
tests. Before the fix, the forced-full commit failed with the exact error
above; after the fix all eight pass:

```text
go test ./internal/storage/postgres/ -run 'TestCommitStoresForcedFullOverMissingBasePrior|TestCommitRefusesEmptySuccessorOverMissingBasePrior|TestCommitFailsClosedOnPriorCountError|TestCommitRefusesEmptySuccessorOverContentBearingPrior|TestCommitRefusesSharplyShrunkenSuccessor|TestCommitStoresTrustedEmptySuccessor|TestCommitStoresEmptySuccessorOverEmptyPrior|TestCommitStoresMildlyShrunkenSuccessor' -count=1
ok  github.com/eshu-hq/eshu/go/internal/storage/postgres
```

The full `go/internal/storage/postgres` suite, the `backfill`, `projector`,
`collector`, and `collector/repo/git` suites, `go vet`, `gofmt`, and
`git diff --check` are clean. The change adds no query, statement, lock, or
worker on any success path: the `errors.As` branch runs only when `CountFacts`
already failed, and the degrade path issues fewer reads than the failure it
replaces, so no plan or benchmark evidence applies.

Observability Evidence (#8362): the degrade records a
`successor_prior_uncountable` ingestion commit stage
(`candidate_generation_id` attached), and the refusal log for an uncountable
prior carries `prior_uncountable=true` alongside `prior_fact_count=0`, so an
operator can tell an unknowable prior from an empty one.
