# #4588 — collector backpressure and per-component quotas

Design ADR. No behaviour changes with this document.

Implementation must not start until a harness exists that exercises **claim
issuance under depth**. The issue names #4579 for this, but #4579 drives
recoverable graph writes and does not touch claim issuance, so it cannot
regression-prove this mechanism (#6026 review). Building that harness is
prerequisite work, listed below with the other two prerequisites.

## The gap

Nothing slows intake. `EnqueueWorkItems`
(`go/internal/storage/postgres/workflow_control.go`) batches inserts but places
no bound on how many work items may exist, and generation retention only prunes
*superseded* generations — so a backlog of live work is never relieved by
retention. A reducer outage under active webhooks grows `fact_work_items` and
the fact tables without limit, and recovery is then gated on claim-query
performance across the backlog it created.

The second consumer is governance: once third-party collectors exist, a buggy
component can flood facts with nothing to throttle it.

## What already exists — corrected after review

An earlier draft of this ADR claimed most of the substrate was already present.
Review showed three of those claims were wrong: I verified the pieces **existed**
without checking what they actually carry. The corrected table is the honest
starting point, and it makes this a larger change than the first draft implied.

| Piece | State | Correction |
| --- | --- | --- |
| The throttle point | **Exists** — `ClaimNextEligible` | unchanged; collectors only work when granted a claim |
| Depth signal *by conflict domain* | **DOES NOT EXIST** | `QueueDepths` is queue→status; `SourceQueueDepths` is queue→source_system→status. Neither carries conflict domain. The only `conflict_domain` grouping is a status-blockage query, not a depth observer. |
| Host→collector pacing channel | **DOES NOT EXIST** | `Status.RetryAfterSeconds` is on the collector's RESULT (`sdk/go/collector/types.go`) — it travels collector→host *after* `RunCollector` has run. `ClaimNextEligible` returns `(WorkItem, Claim, bool, error)` with no retry-after, and the runner backs off on a fixed `PollInterval` when nothing is found. |
| A harness that can regression-prove pacing | **DOES NOT EXIST** | #4579 drives recoverable **graph writes**; it does not exercise claim issuance, so it cannot prove a claim-pacing mechanism. |
| Quota declaration on the manifest | **Absent** | unchanged |
| Anything consulting depth when issuing a claim | **Absent** | unchanged |

So the throttle point exists and nothing else does. Three things must be built
before pacing can work at all, and each is a prerequisite rather than a detail:

1. **A conflict-domain admission key and a depth observer grouped by it.**
   Without this, "pace only the affected domain" is unimplementable and the
   design collapses to global pacing.
2. **A host→collector pacing channel at claim time.** Either an extra return
   value on `ClaimNextEligible` (a grant delay) or an explicit
   claim-denied-retry-after result. The existing field cannot carry it.
3. **A harness that exercises claim issuance under depth**, since #4579 does
   not. Without it there is no way to regression-prove that pacing engages,
   releases, and never wedges.

## Boundary with "Serialization Is Not A Fix" — stated first, not last

This is flow control at **intake**: how fast claims are *issued*. It must never
become a reduction of write concurrency, a worker-count knob, or a batch-size-1
drain. Those are the shapes CLAUDE.md forbids as fixes for MERGE races and
non-idempotent writes, and pacing is dangerous precisely because it *looks* like
it makes those symptoms go away — a paced system produces fewer concurrent
writes, so a write-path race gets rarer and harder to reproduce.

Two rules follow, and they belong in the implementation's review checklist:

1. Pacing may only ever delay a claim **grant**. It may not change worker
   counts, batch sizes, lease durations, or conflict-key partitioning.
2. A write-path defect must never be closed by tuning pacing. If enabling
   pacing makes a correctness symptom disappear, that is evidence of a
   concurrency bug to root-cause, not a fix.

## Design

### Signal: per-domain depth first, global as the ceiling

Two signals, not one, because they answer different questions:

- **Per-conflict-domain depth** is the actionable one. A backlog concentrated in
  one domain means that domain's consumer is unhealthy; pacing every collector
  because one reducer domain is behind punishes unrelated intake.
- **Global depth** is the ceiling that protects the database itself — table
  growth, claim-query cost, retention lag. It applies regardless of which domain
  is responsible.

Pacing engages when **either** crosses its threshold; the per-domain signal
throttles only claims that would enqueue into that domain, the global signal
throttles all claims.

### Thresholds and hysteresis

Two watermarks per signal, never one:

| | Per-domain | Global |
| --- | --- | --- |
| engage above | `high` | `globalHigh` |
| release below | `low` (strictly `< high`) | `globalLow` |

A single threshold oscillates: crossing it pauses intake, the queue drains one
item, intake resumes, the queue refills. The gap between `low` and `high` is
what stops that, and it must be large enough that a release is followed by
meaningful drain rather than immediate re-engagement.

**The latch needs an owner.** Between `low` and `high` the correct decision
depends on whether pacing was already engaged, so that bit is state, not a pure
function of current depth. With more than one coordinator it must live in
shared storage alongside the claim path, not in coordinator memory — otherwise
two coordinators disagree about whether pacing is on and the system oscillates
exactly as a single threshold would.

**Concrete starting values are deliberately not proposed here.** They should be
derived from observed drain rate on a representative backlog rather than guessed
in an ADR, and the claim-issuance harness named as prerequisite 3 is where that
measurement belongs.

Pacing is expressed as a delay on the grant, not as a claim denial: a denial
loses the request, a delay preserves ordering and lets the collector back off
without treating it as failure.

That delay needs a channel that does not exist yet. `Status.RetryAfterSeconds`
cannot carry it — it is on the collector's result, produced after the work ran.
The grant path (`ClaimNextEligible` → runner) has no retry-after, so pacing
requires either an added return value or an explicit claim-deferred result.
Specifying that channel is prerequisite work, not an implementation detail.

### Quotas in the component manifest

Per-component limits declared in the `ComponentPackage` manifest, with operator
override:

- facts per generation
- generations per hour

These are governance bounds on a *single* component, distinct from depth
pacing, which is a system-wide health response. A component within its quota can
still be paced by depth; a component over quota is throttled even on an idle
system, because the point is to bound blast radius from a buggy or hostile
component, not to optimize throughput.

Quota state is per (component, scope) and must survive coordinator restart,
otherwise a crash-looping component gets a fresh budget each cycle — which is
exactly the failure mode quotas exist to bound.

Durability is necessary and not sufficient. The decrement must be **reserved
atomically with the claim grant**, in the same transaction: two replicas can
otherwise both read one remaining generation and both proceed, overshooting the
quota by the number of concurrent claimants. Check-then-grant is the classic
shape of that bug and it is the one thing a quota must not have.

### Telemetry

Queue-level depth gauges exist, but not at conflict-domain granularity (see the
corrected table above), so the per-domain signal needs its own observer. Beyond
the signal itself, what is missing is the ability to answer "is pacing on, and
why" — the question an operator has at 3am:

- `pacing_engaged_total` — counter, labelled by signal (`per_domain` /
  `global`) and by conflict domain. Transitions matter, and a gauge sampled
  between engagements reads as "fine".
- `pacing_active` — gauge, 0/1 per signal. The counter alone **cannot** answer
  "is pacing on right now": it holds the same value during an engagement and
  after it ends. Both are needed — the counter for how often, the gauge for
  now.
- `pacing_delay_seconds` — histogram of the delay actually issued.
- `quota_exceeded` — counter, labelled by component and quota kind.

Without `pacing_engaged`, a paced system is indistinguishable from a slow one:
throughput drops and every component-side metric looks healthy. That is the
observability failure this ADR most wants to avoid, because it is the one that
turns a deliberate mechanism into an unexplained outage.

### When pacing itself misbehaves

The failure story the issue asks for, because a throttle that fails closed is an
outage:

- **Depth query fails or times out.** Do NOT pace. Failing open is correct: an
  unavailable signal is not evidence of a backlog, and pacing on a missing
  reading converts a metrics outage into an intake outage.
- **Pacing engages and never releases.** Bound it, but do NOT release blindly.
  A timer release while depth is still above `globalHigh` admits more work into
  an already-saturated system — precisely the sustained-outage case the ceiling
  exists for. Correct shape: on timer expiry, RE-READ depth; release only if it
  is genuinely below the release watermark, and otherwise stay engaged while
  emitting a distinct stuck-pacing signal. A stuck throttle must be loud, not
  silently lifted.
- **Quota state is unreadable.** Do not blanket-allow. Allowing every claim
  removes the protection in exactly the scenario quotas exist for — a buggy or
  hostile component — since an unreadable quota is indistinguishable from an
  exhausted one. Preferred shape: fall back to a conservative floor rate
  (enough for a healthy component to make progress, far below what a runaway
  needs) and emit a distinct signal. That keeps a coordinator storage fault
  from halting healthy collectors without handing a runaway an unlimited
  budget.
- **Clock skew across coordinators.** `generations per hour` is time-windowed,
  so it needs a monotonic source or a database-side window; wall-clock
  comparison across HA coordinators will double-count or under-count.

## Decisions for the owner

1. **Is the quota schema part of the manifest contract, or operator-side
   config?** In the manifest it is declarative and travels with the component,
   but it becomes a contract surface a third party can under-declare. Operator
   config keeps control local at the cost of discoverability.
2. **Does pacing apply to first-party collectors, or only third-party
   components?** Depth pacing protecting the database argues for all; quotas as
   governance argue for third-party only.
3. **What is the authority for thresholds?** Static config, or derived from
   observed drain rate. Derived adapts to corpus size but is far harder to
   reason about during an incident.
4. **Per-domain granularity: conflict domain or reducer domain?** They are not
   the same partition, and the depth signal is only actionable if it matches the
   unit whose consumer is unhealthy.

## Not in this design

Implementation, and the three prerequisites it depends on. A throttle without a
regression test is a mechanism that can silently stop intake, and the harness is
what makes "pacing engaged and never released" a caught defect rather than an
incident — but that harness has to be built, because #4579 does not cover claim
issuance.

Related: architecture review 2026-07 §E.3 and §F.3, contract-system epic #4566,
Ifá saturation Odù #4579.
