# CI census measurement for #8617

The [four-hour snapshot](8617-census-20261023.json) was collected on
2026-10-23 from 14:25:45 to 18:25:45 UTC using the census implementation at
`90be17bf2a7ea86260ee1a7763c4aea9a399050a`. It is a measurement of the existing
CI topology, not a before/after speedup or the final 24-hour acceptance window.

The population has six merged PR final heads. Five reached all three required
successes by merge, with successful-head p50 32.18 minutes and p95 41.72 minutes;
#8794 remained pending and is censored. The boundary starts at the first observed
PR workflow enqueue, including observed queueing and reruns on that head.
Push-to-dispatch delay is not measured. Reuse and smoke have no observations in
this window, so their percentiles remain absent.

The original snapshot recorded 623 cancelled runs and a 49.33% raw cancelled
share; the original below-15% target remains unmet. Two later cancellations were
excluded from that raw population. Its generated 47 executed jobs and 1162.84
runner-minutes are **invalidated**: the classifier treated `runner_id=0` as an
allocated runner. The reported zero unknown observations are also invalid.
Do not use those derived figures for a decision.

A later re-read of the same creation window used the corrected classifier, but
GitHub's run states had advanced. That retrospective population has 635 cancelled
runs: 578 zero-job, 30 executed, and 27 unknown, with 1811.51 evidenced
runner-minutes. This is a distinct population, so it is not a before/after
change in cancellations or runner cost. The unknown count includes jobs whose
`runner_id=0` supplies no evidence of runner allocation. Runner-minutes sum
observed job occupancy, not elapsed time or a financial cost estimate.

The follow-up classification fix refuses to label an unrecognized successful
publisher description as an ordinary full run. It does not change the recognized
publisher descriptions in this snapshot. Full-day reporting and live positive
reuse/smoke proof remain required on #8617.

Independent review subsequently tightened at-merge history to request all check
attempts and reject incomplete REST pages or job counts below the observed
check-suite count. The retained snapshot is historical validation, not final
post-review or full-day acceptance evidence. Boundary regressions exercise the
production API path for both corrections.

The [retrospective re-read](8617-census-20261023-retrospective.json) was produced
by `fetch_window` and `cancellation_cost` with the original window bounds. The
complete source population was re-read, not reconstructed from the original
623 run IDs.

Future census JSON reports give each enrichment a `completeness` value. A failed
required-green or cancellation API read records `failed` and an `error` string;
the Markdown report warns that its metrics are unavailable while retaining raw
run totals and the other enrichment. A cancellation read records `partial` when
unknown jobs, an exhausted job budget, or missing REST runs leave runner cost
incomplete. Successful complete reads record `complete`. Enrichment failures
also print a warning to stderr. These fields describe report coverage, not CI
improvement or regression.

A failed or incomplete jobs page affects only that cancelled run: it becomes
`unknown`, the request counts against the enrichment budget, and the JSON
records `job_fetch_errors` plus up to three bounded examples with run IDs and
error reasons. The Markdown report warns about these failed fetches.
