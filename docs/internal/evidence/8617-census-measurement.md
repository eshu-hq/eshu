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

Of the raw snapshot's 623 cancelled runs, 576 had zero jobs and 47 had executed
jobs, accounting for 1162.84 evidenced runner-minutes. Two later cancellations
were excluded to preserve the raw population. Runner-minutes sum job occupancy;
they are not elapsed time or a financial cost estimate. The raw cancelled share
is 49.33%, and the original below-15% target remains unmet.

The follow-up classification fix refuses to label an unrecognized successful
publisher description as an ordinary full run. It does not change the recognized
publisher descriptions in this snapshot. Full-day reporting and live positive
reuse/smoke proof remain required on #8617.

Independent review subsequently tightened at-merge history to request all check
attempts and reject incomplete REST pages or job counts below the observed
check-suite count. The retained snapshot is historical validation, not final
post-review or full-day acceptance evidence. Boundary regressions exercise the
production API path for both corrections.
