# #7334 section 1: generation retention candidate selection

Scope: `generationRetentionCandidateQuery` in
`go/internal/storage/postgres/generation_retention_sql.go`, the skip-search
retry loop in `go/internal/storage/postgres/generation_retention.go`, and
`generationRetentionSkipSearchLimit` (removed from
`go/internal/storage/postgres/generation_retention_events.go`). Section 2
(the own-row over-limit narrowed path, the `eshu_search_index_terms`
pre-screen, `row_limit_own_rows`/`cause` telemetry) is a separate PR; nothing
in that path changed here.

Authority: arbiter ruling `arb-7334.md` (decisions 1.1-1.7, section 3) and its
follow-up `arb-7334c.md` (section 4 approves the shipped statement text,
section 6 dispatches this PR for proofs P1-P6, P12, P14, P16).

## The defect (baseline)

The shipped `generationRetentionCandidateQuery`'s `locked_scopes` CTE checked
only that a scope owned *some* old superseded generation (age and status, no
rank or live-work check), then locked the first `BatchGenerationLimit` scopes
by `scope_id ASC`. A scope with a few old-but-not-yet-eligible superseded
generations (rank never clears `MinSupersededGenerations`) could pass that
check and consume the whole scope lock budget, starving scopes that sort
later by `scope_id` but actually own an eligible generation.

Arbiter measurement (`arb-7334.md` E1/E3-old, read-only the QA environment, no timing
claimed): on the QA shape (120 scopes with 3 old-but-retained superseded
generations each, 10 scopes with 30 each, ranks 25-30 eligible), the shipped
statement returned **0 rows** against an independent oracle's **59** eligible
generations, and its lock set was **100 unrelated scope rows** (`a000..a099`),
none of which owned an eligible generation.

## The fix (after)

One eligibility definition (`ranked_superseded_generations` ->
`live_work AS MATERIALIZED` -> `eligible`) feeds both `locked_scopes` and the
candidate list, so they cannot disagree; `locked_scopes` orders
oldest-eligible-first with `scope_id` as tie-break. The final `SELECT` is
byte-identical to the statement shipped before this change, including its
locking clause. Full statement and rationale: `arb-7334c.md` section 4 (the
`t01-7334/t1/proposed-v2-livecte.sql` text), reproduced verbatim (modulo no
placeholder changes were needed) as the new
`generationRetentionCandidateQuery`.

T1 (`arb-7334c.md` section 4, read-only QA plus a local PostgreSQL 18.6
throwaway container, no timing claimed): the new statement's rows equal the
oracle's for limit 100 and limit 3, with and without concurrently held
scopes; its lock set is exactly the scopes owning an eligible generation. Its
plan is flat across all four plan states (never analyzed, forced generic
cold, forced generic analyzed, custom analyzed): fact_work_items buffers
5,326 -> 5,327 cold and 1,171 -> 1,172 analyzed at 10x `fact_work_items`. The
ruling's own original 1.2 text (the per-row correlated form) was rejected: its
cold-plan `fact_work_items` buffers grew 230,005 -> 2,372,599 at the same 10x
(the #6809 class), which is exactly what this PR's `live_work AS MATERIALIZED`
CTE avoids.

Since the fixed statement returns the full eligible set (up to
`BatchGenerationLimit`) in one call, the outer skip-search retry loop over an
ever-growing exclusion list can never discover anything the one call did not
already see (`arb-7334.md` section 3); it and
`generationRetentionSkipSearchLimit` are removed, along with the query's
now-unused exclusion parameter. The row-limit re-check loop over a
shrinking/growing recount (`arb-7127-3d-b`) is untouched.

## No-Regression Evidence

No-Regression Evidence: this PR is a correctness fix (the shipped query
returned the wrong candidate set), not a claimed speedup; the checks below
are exactness, lock-set, and plan-shape (Actual Loops / node presence), never
wall-clock. All proofs ran locally against a throwaway `postgres:18-alpine`
container (`docker run --name eshu7334pg -p 25931:5432 ...`, `POSTGRES_DB=eshu`,
removed after), reachable at `ESHU_POSTGRES_TEST_DSN`, using the migrated
bootstrap schema (`openGenerationRetentionMigratedSchema`, one isolated
schema per test). No timing is claimed; every check below is exactness, lock
set, or plan shape (Actual Loops / node presence), not wall-clock.

```
ESHU_POSTGRES_TEST_DSN=postgresql://eshu:eshu@localhost:25931/eshu?sslmode=disable \
  go test ./internal/storage/postgres/... -run 'GenerationRetention' -count=1
```
-> `76 passed` (unit + live), including:

- `TestGenerationRetentionSelectsEligibleGenerationsAcrossScopesLive` (P1):
  RED subtest runs the frozen pre-fix `legacyGenerationRetentionCandidateQuery`
  against a fixture shaped like the E1 defect (15 ineligible-by-rank scopes
  sorting first by `scope_id`, `BatchGenerationLimit`=15, 10 eligible scopes
  sorting after with 1 eligible generation each) and gets 0 rows, reproducing
  E1 locally. GREEN: the shipped store, same fixture and limit, prunes exactly
  the 10 eligible generations and leaves every other row untouched.
- `TestGenerationRetentionLockSetMatchesEligibleScopesLive` (P2): a second
  session's `FOR UPDATE SKIP LOCKED` probe shows the exact 5 scopes owning an
  eligible generation locked, and a 6th scope whose only old generation has
  live (`running`) work not locked.
- `TestGenerationRetentionFairnessAcrossScopesLive` (P3): 30 scopes with
  `scope_id` order opposed to age; the pruned batch of 10 equals the oracle's
  10 globally-oldest generations, not the 10 that sort first by `scope_id`.
- `TestGenerationRetentionSkipsHeldScopeAndReplacesLive` (P4): a second
  session holds the globally-oldest scope's row uncommitted; the store's pass,
  bounded by a 5s context deadline, prunes the next-oldest candidate instead
  without the deadline tripping (`SKIP LOCKED` never waits).
- `TestGenerationRetentionEvalPlanQualDropsRacedCandidatesLive` (P5): a
  blocking-lock mirror of the final `SELECT` (derived from the shipped
  constant, `FOR UPDATE ... SKIP LOCKED` swapped for a plain `FOR UPDATE`, the
  same technique `TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow` uses
  for the reducer queue) proves a candidate deleted-and-committed, or
  reactivated (status changed off `superseded`) and committed, by another
  session between snapshot and lock is dropped by PostgreSQL's EvalPlanQual
  recheck; a bystander candidate with no race gets exactly one retention
  event.
- `TestSelectCandidatesWithinRowLimitProperties` /
  `TestGenerationRetentionStoreCandidateQueryRunsOncePerPass` (P6): the
  existing property test (unchanged) plus a new unit test replacing
  `TestGenerationRetentionStoreRowLimitSkipStopsAtSearchCap`: an all-over-limit
  backlog now issues exactly one candidate query of 3 arguments (no exclusion
  parameter), not one per exclusion-budget step.
- `TestGenerationRetentionCandidatePlanNeverLoopsFactWorkItemsLive` (P12): the
  `live_work` CTE's `fact_work_items` access node has `Actual Loops = 1` in
  all four plan states; a seeded reversion to the per-row correlated form
  (`generationRetentionCandidateQuerySeededPerRowLiveWork`, derived by two
  exact substring replacements on the shipped constant) fails the same guard
  cold (`Actual Loops` > 1), proving the guard can see the defect it exists to
  catch.

Existing tests unchanged and passing: every `generation_retention*_test.go`
and `freshness/links/retention*_test.go` (`go test
./internal/storage/postgres/freshness/links/... -run Retention -count=1` ->
`18 passed`). `TestGenerationRetentionCandidateQueryProtectsWindowAndLocks`
was updated for the new text it asserts against (`ranked.superseded_at < $1`
in place of `generation.superseded_at < $1`, plus `live_work AS MATERIALIZED`
and `eligible_scopes`); it is a literal-text assertion on the statement this
PR intentionally rewrites, not a behavior regression.

`go build ./...`, `go vet ./internal/storage/postgres/...`, `gofumpt -l
internal/storage/postgres/generation_retention*.go` (clean),
`go test -race ./internal/storage/postgres/... ./internal/reducer/maintenance/...
-count=1` (2622 passed), and the broader no-regression sweep `go test
./cmd/reducer ./internal/reducer/... ./internal/storage/postgres/...
./internal/status ./internal/query ./internal/telemetry -count=1` (4921
passed) all ran clean on the final SHA.

## No-Observability-Change

No-Observability-Change: this PR adds no metric, span, log field, or status
row, and changes no existing one. `GenerationRetentionResult.Skipped["row_limit"]` and the
`eshu_dp_generation_retention_skipped_total{reason="row_limit"}` counter it
feeds are produced by `selectCandidatesWithinRowLimit` and
`rowLimitSkipReason`, both untouched by this PR. Section 2's `cause` label and
`row_limit_own_rows` semantics change are out of scope here.
