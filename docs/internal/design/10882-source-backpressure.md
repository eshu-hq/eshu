# Slow intake from a source when the reducer falls behind on it (#10882)

Status: **Accepted as the design authority for #10882 (arbiter ruling,
2026-11-21)**, except every numeric threshold, which stays **Proposed**
until slice S0/S1 measurement confirms or replaces it (the #10861 ruling
discipline: no numeric limit binds before its baseline). Refs #10882.
Part of #10860 (phase 3, step 3). Depends on #10861 decision 4 (freshness
pairing) and decision 6's skew note, the #10862 freshness target, and the
#10868 split-deployment decision. This record is also the ADR the
`fact_work_items` schema gate requires (`go/internal/storage/postgres/AGENTS.md`,
"What NOT to change without an ADR") for the latch reuse in decision 4:
it adds no `fact_work_items` column, index, or conflict-key change.

## Problem

Nothing slows intake from one source when the reducer falls behind on
it. On 2026-11-17 vulnerability intelligence held 6,363 of 8,628 waiting
reducer items, its oldest 25.8 days old, while cheap git work waited
hours behind the same hand-out. The July 2026 architecture review named
the gap (section E.3, "No collector-side backpressure"; E.2 option 6):
enqueue is batched but unbounded, and recovery from a ballooned queue is
gated by claim performance over the backlog itself.

## What exists today (acceptance sweep against origin/main)

Three mechanisms border this issue. None of them is per-source reducer
backpressure. Every pointer below was read in full for this ruling.

### The 10,000 high-water mark (#10630) is one global ceiling, ingester-only

`go/cmd/ingester/reducer_admission.go` wraps the ingester's
`ReducerIntentWriter`. Before each `Enqueue` it reads two signals from
`QueueObserverStore` (`go/internal/storage/postgres/queue_observer.go`):

- Total outstanding reducer depth — `fact_work_items` rows at
  `stage='reducer'` in `pending|claimed|running|retrying`, restricted
  to active generations (`activeFactWorkItemsCTE`,
  `reducer_generation_filter_sql.go:77`) — summed across **all
  sources**. At or above `ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK`
  (default 10,000) the enqueue defers.
- Graph-write pressure — retrying reducer rows with
  `failure_class='graph_write_timeout'` only (#3560), hysteresis
  500 engage / 100 release.

Deferral is a 1 s poll-wait inside `Enqueue` while the projector
heartbeat keeps the claim alive. After `ESHU_REDUCER_ADMISSION_MAX_WAIT`
(default 5 m, one projector lease) the wait fails as
`ErrReducerAdmissionTimeout` (`reducer_admission_timeout.go:32`):
retryable, attempt-incremented, and — only if the attempt budget is
exhausted — dead-lettered with `failure_class=admission_timeout`
(#10502). Those are the "producer admission_timeout dead letters" in
#10630. Telemetry: `eshu_dp_reducer_admission_deferrals_total{reason}`
plus a structured warn log. Verdict for AC1: **the limit is global, not
per source; one source can use all of it; and it only guards the
ingester's own intent enqueue.**

Two adjacent gaps the sweep found:

- The standalone projector (`go/cmd/projector/runtime_wiring.go:76`)
  enqueues reducer intents through a plain `NewReducerQueue` — **no
  admission gate at all**. Hosted-collector generations therefore reach
  the reducer queue ungated at the last hop.
- The gate guards intent enqueue, not collection. While it defers, the
  git collector keeps committing generations and the projector-stage
  queue grows instead.

### Coordinator claim pacing (#8470) exists — wrong signal, never enabled

`go/internal/workflow/claim_pacing.go` + `CheckClaimPacing`
(`go/internal/storage/postgres/workflow_claim_pacing.go`) implement
exactly the actuator AC3 asks for: a shared-storage hysteresis latch
(`workflow_claim_pacing`), per-family `domain` signal consulted before
a `global` ceiling, work-conserving skip in `FairClaimDispatcher`
(`go/internal/collector/fair_claim_dispatcher.go:148`), grant delay on
the retry-after path (`ClaimPacingDeferral`), fail-open on signal
error, stuck-past-bound stays loud, and a full metric family
(`eshu_dp_workflow_claim_pacing_*`, `docs/public/reference/telemetry/claim-pacing.md`).

But its depth signal is **pending `workflow_work_items` per collector
family** — the collection queue, not the reducer queue
(`workflow_claim_pacing.go:24`). A family can have a shallow collection
queue while its committed generations sit 25 days in the reducer queue;
#8470 cannot see that. And no production wiring enables it:
`ClaimPacingConfig` is constructed only in tests (`DomainHigh:` appears
in four test files, no `cmd/` or entrypoint code, no env vars in
`go/internal/envregistry/entries.go`). The zero value disables pacing,
so every deployment runs unpaced today. AC3 is therefore **not met**:
the mechanism is built, its signal is the wrong queue, and it is off.

Per-component claim quotas (#8471, `workflow_control_quota.go`) cap
generations per hour per component at claim time. That is a runaway
cap, blind to reducer backlog — a different job, unchanged by this
record.

### Per-source reducer queue age and depth are already observable

`fact_work_items` (`schema/data-plane/postgres/005_fact_work_items.sql`)
carries `stage`, `domain`, `scope_id`, `generation_id`, `status`,
`created_at`, `reopened_at` (via later migrations), `visible_at`, and
`payload`. Source attribution already exists in the observer:
`sourceQueueDepthQuery` / `sourceQueueOldestAgeQuery`
(`queue_observer.go:62,79`) derive `source_system` from
`payload->>'source_system'` falling back to
`ingestion_scopes.source_system`, active-generation filtered, and feed
the gauges `eshu_dp_queue_source_depth{queue,source_system,status}` and
`eshu_dp_queue_source_oldest_age_seconds{queue,source_system}`
(`go/internal/telemetry/instruments.go:8615`). Oldest age is
`MIN(COALESCE(reopened_at, created_at))` over outstanding rows. What
does **not** exist: a claimable-only variant (the observer counts rows
whose `visible_at` is in the future and known gated cohorts such as
#10630's driver-deferred Oct-16 rows), any arrival-rate metering (the
#10862 baseline lists arrival rate as not checked; its closure notes
intake metering is still owed), and any per-source slowed state on the
status API (`collector_backpressure` in `/admin/status` reports
workflow claim pressure per family/instance/source —
`go/internal/status/collector/backpressure.go:19` — not reducer pacing).

### The #10862 freshness target and the #10868 split

The target this design must pace against: **per source, 95% of facts
readable within 24 h of save, typical within 4 h** — recorded in the
#10862 baseline (§9) as Proposed pending #10861 acceptance, and
referenced from `docs/internal/design/10861-reducer-execution-model.md`.
Note: the baseline file itself was deleted from the tree by the #10990
prose-evidence cleanup (`b7d15bae70`); the target text survives in git
history and in the #10861 record's references. Restoring or re-homing
that record is owed work (see "What stays unverified").

#10868 is closed with an accepted decision
(`docs/internal/design/10868-vulnerability-split-deployment-decision.md`):
split reducer deployments by domain (heavy vulnerability domains vs
the claimable complement) cut the complement's queue waits ~94% at p50
on the replay seed. So "behind" on the **drain** side is now
per-deployment domain set. That decision itself warns that source
counts and domain filters do not line up, so an **intake** trigger keyed
by reducer domain would misattribute sources. Pacing (intake, per
source) and the split (drain, per domain) are complementary; neither
replaces the other, and this record does not re-decide drain-side
fairness (#8469, #10868 own that).

## Decisions

### 1. Trigger: per-source oldest claimable reducer age, with a depth floor

A source is "behind" when the oldest **claimable** outstanding reducer
row attributed to it (active generation, `status IN
pending|retrying`, `visible_at <= now`, age from
`COALESCE(reopened_at, created_at)`) exceeds the engage age **and** the
source's outstanding claimable depth exceeds a small floor. The latch
releases when oldest claimable age falls to the release age.

- Age is the primary signal because the freshness target is an age. A
  fact saved behind a 1 h-old claimable head cannot beat ~1 h of queue
  wait under any approximately fair drain; pacing must engage well
  before the 4 h typical / 24 h p95 targets are already lost.
- The depth floor exists because one poisoned or deliberately gated
  old row must not throttle a whole source (#10630's deferred cohort
  is the standing counterexample). "Claimable-only" plus the floor
  together make the false-throttle case require a genuinely stuck
  *set* of rows — which is precisely when slowing intake is right.
- Proposed defaults (to be confirmed or replaced by S0/S1, never
  silently shipped): engage age 1 h (¼ of the 4 h typical target),
  release age 15 m, depth floor 25. All three are env-configured,
  validated (release < engage, floor ≥ 1), and 0 engage-age disables
  the per-source leg entirely. The existing global 10,000 ceiling and
  graph-write-pressure gate stay as database-protection backstops,
  unchanged in meaning.

### 2. Fairness: independent per-source latches, work-conserving — no shares

Each `source_system` gets its own latch. There are no fixed shares or
quotas: a source under its age threshold may use all available intake
capacity, and a slowed source's would-be grants go to other families in
the same dispatcher pass (the #8470 work-conserving skip). The fairness
guarantee is the one AC5 needs and no more: **one source's backlog
cannot delay another source's intake**, because no global depth signal
(except the pre-existing database-protection ceiling) feeds a
per-source verdict. Static shares are rejected below.

Family-to-source mapping: a pacing consult arrives as
`(collector_kind, collector_instance_id)`; the verdict is keyed by
`source_system`. Slice S0 must census the live mapping from
`workflow_work_items` (the `BackpressureSnapshot` tuple proves the
join exists). If every family serves exactly one source, the consult
resolves family → source directly. If any family serves several, the
consult must evaluate the **pending items'** sources and defer only
grants for slowed sources (per-item skip), never the whole family —
S2 does not pass review with a whole-family over-throttle of a
multi-source family.

### 3. Actuators: claim grant rate for hosted collectors; admission + schedule for the ingester

- **Hosted collectors** slow at claim issuance, through the existing
  #8470 path: `FairClaimDispatcher` and the undispatched
  `ClaimedService` grant (`claimed_service_grant.go:40`) consult the
  new reducer-source signal alongside the existing workflow-depth
  signals; a paced verdict returns `ClaimPacingDeferral` (retry-after,
  no claim issued, no attempt consumed). Production enablement is part
  of the work: env vars in the registry, threaded through the
  collector entrypoint template, validated at startup like
  `ClaimPacingConfig.Validate` already does. Pacing consults run on
  the claim hot path, so the signal read follows the #8470 precedent:
  one grouped query per consult, depth/age cache TTL, latch always
  read fresh, fail open on read error with the existing debug record.
- **Ingester (git)** slows at two points. (a) The admission writer
  gains a per-source leg: intents whose source is latched slowed defer
  exactly as high-water deferrals do today (poll, heartbeat, bounded
  wait, retryable timeout). (b) The schedule leg AC3 names: while the
  git source is latched, the repo-selection collector stretches its
  per-repo minimum resync interval by a configured multiplier
  (proposed ×4). Selection is deferred, never dropped — due repos,
  reindex requests (#7620: scheduled selection is their only visitor),
  and webhook triggers all remain pending work and are visited when
  the latch releases. The multiplier, not a hard stop, keeps git
  freshness degrading gracefully instead of cliff-edging.
- **Standalone projector** gets the same admission writer as the
  ingester (global backstop + per-source leg). Without it, already
  committed hosted generations bypass pacing at the last hop and AC4/5
  proofs would be measuring a sieve. Intent batches are
  per-generation (single scope, single source); S3 must prove that
  invariant at the writer boundary or split batches by source before
  the gate.

Pacing never reduces worker counts, batch sizes, or concurrency
anywhere. It changes *when* new work is admitted, per source; every
worker stays free to take other sources' work. That is the difference
between backpressure and serialization, and the S2/S3 proofs must show
workers staying busy on unpaced sources while one source is slowed.

### 4. Signal and latch storage: extend what exists

- New store read beside the observer: per-source claimable oldest age
  + claimable depth in one grouped statement over active generations
  (the `sourceQueueOldestAgeQuery` shape plus the claimable
  predicate). S0 proves its plan and cost before any hot-path use
  (prove-the-theory gate: `EXPLAIN ANALYZE` on the #10862 replay
  fixture and a 10× synthetic, consult cost reported against claim
  cost). If the grouped scan cannot stay in the low milliseconds at
  10×, the fallback is a bounded per-source probe for the claiming
  family's sources only — decided by S0 numbers, not preference.
- Latch: reuse `workflow_claim_pacing` with a new closed signal value
  (`reducer_source`) and `domain = source_system`, reusing the
  transition-convergent upsert and the domain-first/global-ceiling
  decision order in `DecideClaimPacing`. Unknown signal values on the
  wire keep the #10377 behaviour (widen to global, log, never a metric
  label). One latch table stays the single answer to "is pacing on,
  and why" across coordinators. No `fact_work_items` schema change.

### 5. Resume: hysteresis release, stuck stays loud, no blind release

Release when oldest claimable age ≤ release age on a consult (the
engage/release gap is the anti-flap mechanism, as in both existing
gates). An engagement older than `MaxEngagement` with age still above
release raises `Stuck` and **stays paced** — copied from #8470, because
releasing blind into a still-saturated source converts a visible
throttle into an invisible queue explosion. Resume needs no new
notification path: collectors already re-claim on their poll loops and
the ingester re-selects on its 1 s collector poll, so a released latch
is observed within one poll + cache TTL.

### 6. No-loss argument (what S2/S3 must prove, not assert)

Pacing deletes nothing and terminal-fails nothing:

- Hosted: a paced grant issues no claim and consumes no attempt; the
  `workflow_work_items` row stays `pending` bit-for-bit. Proof:
  attempt counts and row sets identical before/after a paced window,
  zero dead-letters minted, and after release the same generation set
  commits as in an unpaced control run over the same seed.
- Facts not yet collected stay at the provider. This is safe only
  where collection is re-readable: git by ref state, vulnerability
  windows by cursor that does not advance without commit. S2 names
  the re-read contract for each proof source; a source whose
  collector advances state before commit is **not** eligible for
  claim pacing until that is fixed — pacing must never be the reason
  a window is skipped.
- Ingester/projector admission: the timeout path is attempt-increment
  retry, terminal only at budget exhaustion. S3 must show the
  arithmetic (attempt budget × MaxWait vs worst-case engagement) and a
  slowed-then-released run ending with zero `admission_timeout` dead
  letters and generation sets equal to the control run. If the
  arithmetic cannot guarantee that under the configured bounds, the
  per-source leg must defer by *requeue-without-attempt* semantics
  instead — decided in S3 by measurement, before enforcement ships.

### 7. Publication: metrics first, then the status surface

- Metrics: the existing pacing family gains
  `pacing_signal="reducer_source"`, `domain=<source_system>` series
  (engaged/active/stuck/delay), a depth-behind-verdict gauge per
  source following the #8472 pattern, and the trigger gauges already
  exist (`eshu_dp_queue_source_oldest_age_seconds`,
  `eshu_dp_queue_source_depth`). Ingester/projector deferrals extend
  `eshu_dp_reducer_admission_deferrals_total` with
  `reason="source_behind"` + source label, following the existing
  reason/failure-class convention. New env vars and metrics land in
  the env registry, telemetry reference docs, and per-package
  docs in the same PRs that add them (repo documentation discipline).
- Status API: the coordinator section of `/admin/status` gains a
  `source_pacing` list — per source: slowed or not, signal, oldest
  claimable age, claimable depth, engaged-since, stuck — read from
  the latch + observer, credential-safe like `BackpressureSnapshot`.
  OpenAPI, MCP, and HTTP reference move in lockstep (repo rule).
  Operators can also read `workflow_claim_pacing` directly; the API
  is convenience, the latch is truth.

## Rejected alternatives

| Alternative | Why not |
| --- | --- |
| Keep only the global 10,000 ceiling | It is the bug: one source consumed it while others starved; also ingester-only. |
| Depth-only per-source trigger | Depth is the wrong proxy twice over: a deep fast-draining queue is fresh, a shallow stuck one is stale; and the #10862 replay proved hand-out cost does not grow with depth (0.79× flatness) — contention, not depth, is the cost. Depth survives only as the anti-poison floor. |
| Arrival-vs-handled rate trigger | No arrival metering exists (owed since #10862); a rate verdict needs windowing state, lags the backlog it must prevent, and cannot be tied to the age-based freshness target without a drain model we have not measured. Revisit once intake metering lands; the latch/actuator design admits it as a second signal later. |
| Fixed per-source shares / quotas | Non-work-conserving: idle share is wasted while another source waits. #8471 quotas already cover the runaway-component case. Fairness here is isolation, not rationing. |
| Reducer claim-side fairness only | Reorders the drain; does not bound intake, fact-table growth, or recovery time after an outage (E.3). Drain fairness is #8469/#10868 territory and already decided there. |
| Collector-local buffering | Collectors hold no queue or reducer handles by ecosystem design; the database stays the queue (#10860). Pacing must act through claims and schedules — which is what AC3 says. |
| Broker or second database | Refused by the #10861 ruling; measurements do not support either. |
| Worker reduction / single-threaded drain | Serialization is not a fix (repo rule). Pacing keeps every worker available to unpaced sources. |

## Slice plan (ordered; each slice is one PR, TDD failing-test-first)

**S0 — Theory shim and census (no production behaviour).**
Files: test-only additions under `go/internal/storage/postgres/`
(reuse the #10862 replay harness, `reducer_queue_10862_replay_test.go`)
plus an evidence note in this record's PR.
Proof: `EXPLAIN ANALYZE` + wall cost of the claimable per-source
age/depth query at replay scale (8,713 rows) and 10× synthetic,
reported against claim-query cost on the same fixture; family→source
mapping census query and its result shape; verdict simulation over
seeded queue shapes showing engage/release on age with the floor
suppressing the single-poison-row case. Gate: if consult cost is not
low-millisecond at 10×, S1 uses the per-family bounded probe.

**S1 — Signal, latch, observe-only publication.**
Files: `go/internal/workflow/claim_pacing.go` (signal widening),
`go/internal/storage/postgres/workflow_claim_pacing.go` + new
observer read in `queue_observer.go` (or sibling file under the
500-line rule), `go/internal/telemetry/instruments.go` +
`contract.go`, env registry entries, telemetry docs.
Proof: unit verdict tests (engage/release/stuck/fail-open); live-store
tests on seeded `fact_work_items` shapes including stale-generation
rows that must not count; shadow-mode proof that claims granted with
the signal wired are identical to claims without it (observe-only —
verdicts computed and metered, never acted on).

**S2 — Hosted enforcement at claim grant.**
Files: `go/internal/collector/fair_claim_dispatcher.go`,
`claimed_service_grant.go`, `claimed_multi_source_host.go`,
collector entrypoint template + env plumbing
(`go/internal/collector/entrypoints/`), claim-plane client parity if
the consult crosses that boundary.
Proof (queue rules: built binary, competing workers, worst-case
backlog): two-source replay run — source A seeded past the engage age,
source B healthy. Assert: A grants paced (deferrals metered, attempt
counts unchanged, zero dead-letters), B's claim rate and queue-wait
p50/p95 unchanged within the baseline tolerance vs control, workers
busy on B while A is paced (no serialization), release resumes A, and
final committed generation/fact sets equal the unpaced control run.

**S3 — Ingester + projector admission leg and git schedule stretch.**
Files: `go/cmd/ingester/reducer_admission*.go`, ingester wiring +
repo-selection cadence (`go/cmd/ingester/wiring.go`, collector
selection config), `go/cmd/projector/runtime_wiring.go`,
`go/internal/projector/runtime/projection.go` if the writer contract
needs the source key.
Proof: git source latched → deferrals counted under
`reason="source_behind"`, selection interval stretched and restored,
zero `admission_timeout` dead letters after release, generation sets
equal to control; projector path identical assertions; batch
single-source invariant test at the writer boundary; attempt-budget
arithmetic recorded in the PR (decision 6).

**S4 — Status publication.**
Files: `go/internal/status/` (snapshot + JSON + render),
`go/internal/query/status_mappers.go`, OpenAPI under
`go/internal/query/openapi/`, MCP status tool parity,
`docs/public/reference/http-api.md`, service-runtimes and telemetry
docs.
Proof: handler tests for slowed/not-slowed/stuck rendering, scoped
-token redaction parity with `collector_backpressure`, openapi
lockstep gates, docs build.

Every slice: focused tests first (failing regression before fix
behaviour), `git diff --check`, and the registry-selected static
gates for the touched packages. S2/S3 additionally carry the
concurrency proofs above — row-set equivalence alone is not accepted
for claim-path changes (repo evidence rules).

## Acceptance-criteria disposition for #10882

| AC | Disposition |
| --- | --- |
| Trace the existing limit, per source or overall | Done above: global total outstanding depth ≥10,000 + graph-write-timeout retrying 500/100, ingester intent-enqueue only; projector ungated. |
| Decide the trigger | Decision 1: per-source oldest claimable age with depth floor; thresholds Proposed pending S0/S1. |
| Slow hosted via coordinator, ingester via schedule | Decision 3 (S2/S3); note the actuator already exists (#8470) — the work is the signal, the mapping, and production enablement. |
| Prove no loss + resume | Decision 6 proof obligations in S2/S3. |
| Prove other sources' freshness unchanged | S2 two-source control comparison (queue-wait p50/p95, claim rate) — freshness read as fact-save→readable per #10862's definition where the harness supports it, queue-wait as its bounded proxy otherwise, labelled as such. |
| Publish per-source slowed state + reason | Decision 7 (S1 metrics, S4 status API). |

## Implementation evidence (2026-11-21, executor slices)

No-Regression Evidence: the per-source leg ships disabled. With the
zero `SourceBackpressureConfig` every consult returns before touching
storage (`TestCheckSourceBackpressureDisabledReadsNothing`: 0 queries,
0 latch writes), so baseline and after behavior on every existing claim,
admission, and status path are identical until an operator sets all
`ESHU_SOURCE_BACKPRESSURE_*` thresholds. When enabled, the added cost per
consult is bounded: one grouped claimable-signal read over the bounded
claimable backlog using the per-row active form (the #6794
selective-probe precedent, cacheable via
`ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL`), one read of the small
`workflow_claim_pacing` latch rows, and latch writes only on transitions.
The status snapshot gains exactly those two reads (28 → 30 round trips,
pinned in `TestReadStatusSnapshotEvaluatesActiveWorkOnce`, which also
pins the materialized scope-state build at one evaluation). What is NOT
measured yet: the S0 `EXPLAIN ANALYZE` of the signal query at replay and
10× scale — thresholds therefore stay Proposed with no defaults, and
enabling the leg in a deployment still owes that measurement. Focused
suites green at implementation: `internal/workflow`,
`internal/collector` (including `-race` on the dispatcher proofs),
`cmd/ingester`, `internal/status/...`, `internal/query`, and
`internal/storage/postgres` minus three pre-existing environmental
failures (host-load ceiling, deploy-checkout env).

Observability Evidence: slowed engagements, releases, stuck verdicts, and
paced-grant delays ride the existing claim-pacing series with
`pacing_signal="reducer_source"`, `domain=<source_system>`; ingester
deferrals add `reason="source_behind"` with a `source_system` label on
`eshu_dp_reducer_admission_deferrals_total` plus the structured warn log;
fail-open consults emit the debug record `source backpressure consult
failed open`; the coordinator status surface publishes `source_pacing`
rows (slowed, reason, oldest claimable age, claimable depth, engaged
since) in JSON, text, and the query status mapper.

## What stays unverified

- All thresholds (engage/release ages, depth floor, schedule
  multiplier, cache TTL) are Proposed. No live or replay measurement
  of the new signal exists yet; S0/S1 own that. Nothing in this record
  authorizes shipping enforcement defaults before those numbers.
- The #10862 freshness target itself is still Proposed in #10861
  (never formally accepted), and its baseline record was deleted from
  the tree by #10990. If #10861 later sets a different target, the
  engage age is re-derived from it (¼ of the typical figure);
  re-homing the baseline record is owed outside this issue.
- QA production readings are taken from the issues' 2026-11-17/18
  read-only measurements; this ruling verified code, schema, and
  documents on `origin/main`, not live QA state, and not the
  eshu-workflow satellite's claim-plane copy of the pacing consult —
  S2 must confirm satellite parity or scope enforcement to in-repo
  claim paths and say so.
- Per-source re-read safety (decision 6) is asserted for git and
  vulnerability windows from their commit/cursor design as read here;
  the S2 proof sources must re-verify it in their own harness before
  pacing is enabled for them.
