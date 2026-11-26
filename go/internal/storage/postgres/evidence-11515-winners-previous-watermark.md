# #11515 winners PreviousWatermark pre-read (no-regression record)

The three winners stamp sites RETURNING-scanned the post-upsert watermark,
so `PreviousWatermark` was structurally the new watermark and
`previous_watermark_age_seconds` read 0 on every advancing tick. The fix
pre-reads the pre-stamp value with `scanWinnersTime` in the same transaction
immediately before each stamp statement
(`stampWinnersWatermark`,
`applyWinnersKeyed`,
`runSupplyChainImpactWinnersRebuild`) and reports it, keeping the RETURNING
scan for arity. Zero SQL-text change: the three statements are byte-identical,
so every pinned plan is unchanged.

## No-Regression Evidence:

Baseline: `origin/main` at `df49648c1d` (worktree base). After: this branch
HEAD. Backend: scripted fakes (no live PostgreSQL on this host; the live
winners suites run in CI). Input shape: sequencing fakes where the pre-stamp
read returns R and every stamp RETURNING returns C > R, plus the empty-table
first-sweep shape and the updated `winnersFakeTx` (pre-read scripted
separately from the committed read). Terminal counts: the RED run failed all
three new stamp tests pre-fix (`PreviousWatermark == C`, want R); post-fix
`TestStampWinnersWatermarkReportsReplacedValue`,
`TestStampSitesReportPreStampOnAdvance` (apply + rebuild arms),
`TestStampWinnersWatermarkFirstSweepIsZero`, and
`TestResweepWinnersReportsCounts` pass, the full
`go/internal/storage/postgres` package passes except the pre-existing
environmental `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`
(needs `ESHU_DEPLOY_CHECKOUT`; fails identically on the base tree), and
`go/internal/reducer/supplychain/core` plus `go vet` on both packages pass.
Why safe: the only added work is one primary-key singleton
(`WHERE singleton = 1`) SELECT per tick in the tick's own transaction — no
new write, lock, plan, or retry; the stamp statements, their order, and the
commit boundary are untouched.

## Observability Evidence:

`previous_watermark_age_seconds` (`winners_maintainer_emit.go`) is
`stamp.Sub(PreviousWatermark)`: on a clamped tick it reports 0 with
`PreviousWatermark == R`, and on an advancing tick it now reports the true
C-minus-R delta instead of the structural 0. The emit path itself is
unchanged; the new store tests pin both sides, and the existing maintainer
signal assertions (`winners_keyed_horizon_test.go`,
`winners_maintainer_signals_test.go`) still pass unmodified.
