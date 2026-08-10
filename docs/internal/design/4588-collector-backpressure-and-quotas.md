# #4588 — collector backpressure and per-component quotas

Design ADR. No behaviour changes with this document. Implementation is gated on
#4579 (the Ifá saturation Odù) landing first, so the mechanism has a harness
that can regression-prove it before it can throttle anything in production.

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

## What already exists — verified

| Piece | State | Location |
| --- | --- | --- |
| Global + per-queue depth signal | **Exists** | `QueueDepths`, `SourceQueueDepths` |
| The natural throttle point | **Exists** | `WorkflowControlStore.ClaimNextEligible` — collectors only work when granted a claim |
| Slow-path response to a collector | **Exists** | `Status.RetryAfterSeconds` (SDK `collector/types.go`) |
| Batched enqueue | **Exists, unbounded** | `EnqueueWorkItems` |
| Quota declaration in the manifest | **Absent** | `Manifest`/`Spec`/`RuntimeContract` carry no quota or pacing field |
| Pacing decision | **Absent** | nothing consults depth when issuing a claim |

So the signal, the throttle point, and the collector-facing response all exist.
What is missing is the decision that connects them, and the declaration that
bounds a single component. That is a smaller change than the issue's framing
implies, and it is worth stating because it sets what the ADR has to specify:
policy, not plumbing.

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
meaningful drain rather than immediate re-engagement. **Concrete starting
values are deliberately not proposed here** — they should be derived from the
observed drain rate on a representative backlog, not guessed in an ADR, and the
#4579 harness is where that measurement belongs.

Pacing is expressed as a delay on the grant, surfaced through the existing
`Status.RetryAfterSeconds`, not as a claim denial. A denial loses the request; a
delay preserves ordering and lets the collector back off without treating it as
failure.

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

### Telemetry

Depth gauges already exist. What is missing is the ability to answer "is pacing
on, and why", which is the question an operator has at 3am:

- `pacing_engaged` — counter, labelled by signal (`per_domain` / `global`) and
  by conflict domain. A counter, not a gauge, because the transitions are what
  matter and a gauge sampled between engagements reads as "fine".
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
- **Pacing engages and never releases.** Bound it. A maximum continuous
  engagement duration, after which pacing releases and emits a distinct
  counter — a stuck throttle must be loud, not silent. The release is safe
  because the ceiling exists to protect the database, and an unbounded pause
  protects nothing while intake is already stopped.
- **Quota state is unreadable.** Allow the claim and count it. A component
  cannot be held responsible for coordinator state it cannot see, and the
  alternative is a coordinator bug silently halting a healthy collector.
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

Implementation. Per the issue, this lands after #4579 provides the saturation
harness — a throttle without a regression test is a mechanism that can silently
stop intake, and the harness is what makes "pacing engaged and never released"
a caught defect rather than an incident.

Related: architecture review 2026-07 §E.3 and §F.3, contract-system epic #4566,
Ifá saturation Odù #4579.
