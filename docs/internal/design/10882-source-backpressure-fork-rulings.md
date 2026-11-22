# #10882 fork rulings and acceptance sweep (2026-11-22)

Status: Decided on the arbiter ruling in `.claude/arbiter-10860-drive-plan.md`.
Companion to [10882-source-backpressure.md](10882-source-backpressure.md),
which holds the trigger, actuators, and slice plan. This file records the
three forks the arbiter named before the PR, with the proof behind each, and
re-sweeps the six acceptance criteria.

## Fork 1: pacing must not mint dead letters

**Finding.** The per-source admission leg shared the high-water timeout path.
`enqueueBySource` sent a `source_behind` deferral through
`admissionTimeoutExceeded` (`go/internal/reduceradmission/`). After
`ESHU_REDUCER_ADMISSION_MAX_WAIT` (5 minutes) it returned the admission
timeout, which spends one wait of the finite admission budget (48 by default,
roughly 44 to 46 hours to exhaust, `projector_queue_admission_budget.go`).
`MaxEngagement` does not release a paced source: an engagement older than it
raises `Stuck` and stays paced. So a source slowed for longer than the budget
would dead-letter its own items as `admission_timeout`, which breaks AC4.

**Decision.** A `source_behind` wait that outlasts `MaxWait` returns a
distinct `reducerSourcePacedError` (retryable, `failure.SourcePacedError`,
no `FailureClass`). It is not an `AdmissionTimeoutError`. The projector queue
treats it as a third retry kind:

| Retry kind | Counter it increments | Dead-letters? |
| --- | --- | --- |
| projection failure | none (spends `attempt_count`) | when projection claims reach `MaxAttempts` |
| admission timeout | `admission_wait_attempts` | when the admission budget is spent |
| source paced (new) | `source_paced_waits` (migration 331) | never |

Projection claims are now `attempt_count - admission_wait_attempts -
source_paced_waits`, so a paced wait is refunded from the projection budget
the same way an admission wait is. It has a counter of its own so a long
engagement cannot spend the admission budget a later genuine admission
timeout owns. `attempt_count` stays a pure claim fence; no decrement, so no
ABA reuse of a fence value. The retry backs off on `source_paced_waits`, so a
long engagement settles at the retry cap.

**Bound.** Pacing is bounded by the source's latch, not an attempt count.
`Stuck` stays loud on the claim-pacing series and `/admin/status`
`source_pacing`; a paced item also shows on
`eshu_dp_projector_source_paced_retries_total` and on its
`fact_work_items.source_paced_waits` row.

**Proof.**
- `TestReducerAdmissionSourceBehindTimeoutIsSourcePacedNotAdmissionTimeout`:
  a source slowed past `MaxWait` returns the paced error, never
  `ErrReducerAdmissionTimeout`, and admits nothing.
- `TestProjectorQueueSourcePacedSlowerThanBudgetsLosesNothing`: 1,000 paced
  cycles (about 20 times the default admission budget) produce zero dead
  letters, leave `admission_wait_attempts` at 0, and after release the item
  still has its full projection budget and full admission budget.
- `TestProjectorQueueFailSourcePacedNeverDeadLettersAndSpendsNoBudget`: even
  with every counter far past both budgets, `Fail` takes the retry path and
  increments only `source_paced_waits`.
- Both RED before the change (compile failure on the missing contract), GREEN
  after.

**Residual.** Two costs remain, both tracked in #11186 (F8):
- *Repeated write cost.* The admission gate sits after the canonical and
  content writes (`projector/runtime/projection.go`), and source-paced
  retries are unbounded, so every paced cycle of an item repeats its fact
  load, build, canonical graph write, and content write before the wait,
  for as long as the latch holds. `Stuck` does not release it.
- *All workers parked.* A paced item parks its projector worker for up to
  `MaxWait` per cycle. The claim pool's per-source in-flight rank
  (`projector_source_inflight_count`) steers a healthy source ahead of it,
  but with at least as many paced items as projector workers every worker
  can sit in a paced wait and a healthy source's claims queue behind them.

The built-binary replay must measure both: graph and content writes for the
slowed source during an engagement, and the healthy source's queue-wait
p50/p95 with paced items at least equal to the worker count. The fix
direction is a consult before the writes or before the claim, and a release of
the claim on the source-paced path with `visible_at` at least `GrantDelay`
out.

## Fork 2: multi-source families

**Census.** `CheckSourceBackpressure` paces a family only when every pending
source of `(collector_kind, collector_instance_id)` is slowed
(`familyPendingSources`). A family serving two sources is therefore
under-paced (a healthy source keeps the whole family claiming), never
over-paced. The census read every coordinator site that sets a work item's
`source_system` (`go/internal/coordinator`, 44 `workflow.WorkItem` literals):

- Every planner sets `string(scope.Collector<Kind>)`, a fixed constant, so
  each family is one source.
- The component-extension planner takes it from the instance's activation
  host claim (`componentExtensionClaimIdentity`): one instance, one config,
  one source.
- Activation-derived vulnerability work takes the host source system, and
  `checkActivationHost` plus `TestActivationDerivedWorkRefusesForeignSourceSystem`
  pin it to `vulnerability_intelligence`.

**Decision.** Every live family is single-source, so the per-item skip is not
built. `TestWorkflowFamilySourceCensusIsSingleSource` walks the coordinator
sources and fails loudly, with the decision to make, the moment a site sets
`source_system` from anything but a collector-kind constant or an allowlisted,
reasoned site. Seeded RED: renaming an allowlist key fails the test; the
clean tree passes. `TestCheckSourceBackpressureMixedFamilyStaysUnpaced`
continues to pin the never-over-throttle behavior.

**Residual.** If a multi-source family is ever added, the per-item skip needs
claim selection to exclude slowed sources inside the claim query. That is not
designed here; the guard is what forces the decision.

## Fork 3: satellite parity

**Finding (read-only, `eshu-workflow` at `678c1e1`).** Collectors whose claims
run through the claim plane (`ESHU_WORKFLOW_CLAIM_PLANE_URL`) hold a
`workflow.ClaimPlaneClient`, not the Postgres control store.
- `ClaimPlaneClient` has `CheckClaimPacing` but no `CheckSourceBackpressure`.
- The satellite's `workflowv1/pacing.go` knows the `domain` and `global`
  signals only. It has no `reducer_source`, and its store does not read the
  core reducer queue, which is where the signal lives.

**Decision.** AC3 is scoped to in-repo claim paths: the direct
`WorkflowControlStore` claim path (dispatcher, direct grant, multi-source
host) and the ingester. With the leg enabled and a plane-held claim store,
the wiring refuses startup (`entrypoints/service_template.go`,
`cmd/collector-component-extension/service.go`, multi-source host) instead of
claiming unpaced, so the gap is loud. `TestClaimPlaneClientDoesNotServeSourceBackpressure`
fails if the client ever gains the consult, which forces this note to be
revisited. Plane-side pacing needs a `reducer_source` signal and a reducer
queue read in the satellite; that is a separate eshu-workflow change and is
not part of this PR.

## Acceptance sweep at this HEAD

| AC | State | Evidence |
| --- | --- | --- |
| 1. Trace the existing limit | Done | Design record "Acceptance-criteria disposition" row 1: global total outstanding depth at the 10,000 mark plus the graph-write retrying marks (500/100), ingester intent enqueue only. |
| 2. Decide the trigger | Done | Per-source claimable age with a depth floor counted over aged rows (review F5): `DecideSourceSlowed` (`go/internal/workflow/source_backpressure.go`), `TestDecideSourceSlowedOneOldRowAmongFreshRowsDoesNotEngage`, `TestDecideSourceSlowedFloorOneReproducesAgeOnlyVerdicts`, `TestDecideSourceSlowedHysteresis`. Thresholds stay Proposed and ship disabled. |
| 3. Slow hosted collectors via the coordinator, the ingester via its schedule | Done for in-repo paths; plane-held claims out of scope (fork 3) | `TestFairClaimDispatcherSourceBackpressureSkipsSlowedOnly`, `TestSourceBackpressureGrantDeferralDirectPath`, `TestReducerAdmissionSourceBehindDefersOnlySlowedSource`, `TestApplySelectionBackpressureStretchesWhileSlowed`. |
| 4. A slowed source loses no facts and resumes | Unit and arithmetic proof done; built-binary replay owed | Fork 1 tests above; `TestFairClaimDispatcherSourceBackpressureDefersAndResumes`, `TestCheckSourceBackpressureForSourceReleasesAndResumes`. |
| 5. Slowing one source leaves the others unchanged | Unit proof done; built-binary measurement owed | `TestCheckSourceBackpressureMixedFamilyStaysUnpaced`, `TestDecideSourceSlowedIsPerSource`, mixed-batch admission test. |
| 6. Publish whether each source is slowed, and why | Done, including the stored-summary path (review F7) | `TestCoordinatorSummaryStoresStuckWhenThresholdsAreConfigured`, `TestReadSourcePacingStatusPublishesLatchTruth`, `TestRenderStatusIncludesSourcePacing(Stuck)`, `TestOpenAPISourcePacingMatchesTheWireType`, `reason="source_behind"` on `eshu_dp_reducer_admission_deferrals_total`. |

## Still owed before the issue closes

- A built-binary two-source replay (source A past the engage age, source B
  healthy) asserting for AC4: paced grants with no attempt change, zero
  dead letters, release resumes, committed generation and fact sets equal an
  unpaced control; and for AC5: B's claim rate and queue-wait p50/p95 within
  baseline tolerance with workers busy on B. Merge may ship the leg
  disabled; closing the issue may not.
- Done 2026-11-22: the live-Postgres run of the admission budget and
  source-paced tests against migration 333 passes (exit 0, private
  `postgres:18`, `ESHU_PROJECTOR_ADMISSION_PROOF_DSN`), and the S0 cost and
  bounded-equals-grouped runs pass after review F5 (see below).
- Production-shape confirmation of the Proposed thresholds before any
  deployment enables the leg.
- **#11186 (F8) and #11187 (F9) must close first.** F8: a paced item repeats
  its projection writes and parks a worker (Residual above). F9: the
  claimable signal counts rows the reducer cannot take now (conflict-fenced
  siblings, readiness-blocked retrying rows), so a latch can stay engaged on
  rows the paced intake would never clear. Both must close before the
  built-binary replay is accepted, before #10882 closes, and before any
  deployment sets the `ESHU_SOURCE_BACKPRESSURE_*` thresholds. This PR ships
  the leg disabled and must not be read as closing AC4 or AC5.

## Review F5: aged-row floor, live S0 re-run (2026-11-22)

The verdict now reads two `COUNT(*) FILTER` columns (rows past the engage
age, rows past the release age) in the grouped query and the bounded probe.
Live Postgres 18 in a private container, local host at about 55% IO
pressure, one run each, same container and fixture, `TestSourceBackpressureS0SignalCost`:

| Query (10x scale, 7 sources, about 13k claimable rows for one source) | Before F5 (`8690012e03`) | After F5 |
| --- | --- | --- |
| Grouped signal p50 (publication) | 224.5 ms | 234.3 ms |
| Bounded per-source probe p50 (claim-hot consult) | 88.8 ms | 89.4 ms |
| Claim-probe control p50 (unchanged query) | 10.2 ms | 10.9 ms |

The aggregates add no measurable cost; the plan is dominated by the three
per-row index scans, unchanged by F5. These absolute numbers are higher than
the 146 ms and 41 ms the first S0 recorded because the host is busier now; the
claim-probe control moved with them. They are a same-session before and after,
not a timing claim.
`TestSourceBackpressureProbeMatchesGroupedAndVerdicts` passes: bounded and
grouped agree on depth, oldest age, and both aged-row counts for every seeded
source, and the new `busy` shape (one 3-day-old row among 200 fresh rows) is
not slowed.
