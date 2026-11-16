# NVD baseline checkpoint: complete at start, floor to the millisecond (#10761)

This change fixes three review findings on the baseline lane (F3, F4, F7 of
#10728). `NVDBaselineCommitPolicy.CommitPolicy` in
`go/internal/collector/extensionhost/nvd_baseline_cursor.go` now accepts a
complete verdict that lands exactly on the chunk start, floors `advance_to` to
whole milliseconds before comparing and storing it, and logs one WARN with a
bounded reason when a verdict is held as `not_covered`. The planner floors the
stored resume the same way. The checkpoint compare-and-advance SQL is unchanged;
it gets live Postgres proof. The lane has no production caller until #10746.

## Performance Evidence

No benchmark was run. The added work per baseline commit is one time
truncation and one integer comparison; the reason helper and the WARN run only
on the held path. The planner adds one truncation per plan. No query, join,
index, lock or round trip is added, and the SQL text did not change.

## No-Regression Evidence

No-Regression Evidence: the compare-and-advance contract is pinned against a
real PostgreSQL 18.6 (a throwaway local database, an isolated schema, five live
tests). The tests pass on the real SQL. Each of four SQL mutants fails a live
test: removing GREATEST from `resume_unix_micro` fails three tests; replacing
the sticky `year_complete` OR with a plain assignment fails three; changing the
sha CASE `>=` to `>` fails one; removing GREATEST from `updated_at` fails one.
Go-side mutants (reverting the production hunks, `>` to `>=`, dropping either
floor, `complete &&` to `complete`, truncate to round) each fail a unit test.
Concurrent and overlapping-transaction cases hold: one row per run and year, and
the final resume is the maximum whatever the commit order.

## Observability Evidence

Observability Evidence: a held verdict is no longer silent. It logs
`nvd baseline generation not covered by commit policy, checkpoint holds` at
WARN with `reason` set to one of `advance_missing`, `sha_malformed`,
`advance_behind_start` or `advance_not_past_start`, plus the run, collector
instance, work item and generation identifiers, and never the sha. The
`eshu_dp_nvd_baseline_advance_total{outcome="not_covered"}` counter is
unchanged; the log says which cause. A lane stalled on a chunker that never
advances shows as repeated `advance_not_past_start`.

## Not checked

- Behavior on a deployed host; the lane has no production caller yet.
- The live lane (`live-postgres-readiness`) does not gate merges; its result on
  the pushed head must be read directly.
- Conformance of the collector #100 chunker to the `advance_to` contract; that
  chunker is not built here.
