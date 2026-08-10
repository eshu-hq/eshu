<!-- docs-catalog
title: Performance SLO Contract
description: Publishes the SLO targets for the scale-lab metrics, each with its measurement evidence or an explicit statement that no absolute target is published yet.
type: reference
audience: practitioner, maintainer
entrypoint: false
landing: false
-->

# Performance SLO contract

This is the published performance contract for the scale-lab metrics defined in
`specs/scale-lab-corpus.v1.yaml`. It answers one question per metric: what
counts as acceptable, and what evidence says so.

Every target here is either a rule the spec already commits to, or a number that
was measured and committed. Where neither exists, the row says so plainly rather
than publishing an estimate. A target nobody measured is worse than an absent
one, because it looks like a contract and behaves like a guess.

For the slot taxonomy, see
[Scale slots and the perf contract](scale-slots-and-perf-contract.md). This page
does not repeat that table.

One naming caution. `go/internal/perfcontract` owns these two words, and
`Threshold.Enforcement` applies them per threshold for the local-envelope,
claim-latency, and hybrid-retrieval contracts. `go/internal/ifa/slots.go`
imports that same type and reuses it to classify *slots*. Neither set covers the
eleven scale-lab metrics, so the Enforcement column below is this page's own
classification rather than a value read out of code. Read it as "can a
credential-free CI gate measure this metric today", and expect a metric marked
operator-gated to still be partly asserted inside a hermetic slot — Ifá's
hermetic slots already check backpressure and dead-letter shape, for instance,
without measuring the wall-clock numbers.

## The default target is the accepted baseline

Most scale-lab metrics are defined relative to a baseline rather than as an
absolute ceiling. The governing rule is `runtime_no_regression`:

> Same-shape performance must not regress by more than 10 percent or 60 seconds
> without a tracked owner-approved exception and follow-up issue.

"Same-shape" is load-bearing. A comparison is only valid against a run with the
same corpus, profile, topology, storage state, and start and terminal events.
Comparing across shapes produces a number that looks like a regression or a
speedup and means neither.

It reads similarly to the reducer claim-latency budget, but that budget is a
different contract on a different surface and the resemblance is misleading:
there, p95 claim latency must stay within 1.10x of the same-shape baseline, and
*max* claim latency must not increase by more than 60 seconds at the largest
measured depth. The 60 seconds bounds an increase in the maximum, not an
absolute p95 ceiling. See
[Reducer claim-latency gate](reducer-claim-latency-gate.md).

## Per-metric targets

| Metric | Stage | Target | Enforcement | Evidence |
| --- | --- | --- | --- | --- |
| `fact_rows_per_second` | ingestion | Nonzero, within 10% of accepted same-shape baseline | operator-gated | No committed baseline yet |
| `queue_claim_latency_p95_ms` | queue | p95 within 10% of accepted same-shape baseline | operator-gated | No committed baseline yet |
| `reducer_drain_seconds` | reducer | Spec bar: within 10% or 60 s of accepted baseline, whichever is larger. The projection-tail contract is stricter and governs — see below | operator-gated | No committed baseline yet |
| `graph_write_p95_ms` | graph | p95 within 10% of accepted same-backend baseline | operator-gated | No committed baseline yet |
| `api_p95_ms` | api | p95 within route-specific budget, no unbounded response | operator-gated | Per-capability budgets in `specs/capability-matrix.v1.yaml` |
| `mcp_p95_ms` | mcp | p95 within tool-specific budget, no unbounded response | operator-gated | Per-tool budgets in `specs/capability-matrix.v1.yaml` |
| `retry_count` | queue | **Zero** unexpected retry rows in terminal representative proof | operator-gated | Spec absolute; see `queue_terminal_state` |
| `dead_letter_count` | queue | **Zero** dead-letter rows in terminal representative proof | operator-gated | Spec absolute; see `queue_terminal_state` |
| `memory_high_water_mb` | runtime | Within profile budget, captured for each measured runtime | operator-gated | No committed budget yet |
| `correlation_fanout_candidates_p95` | correlation | p95 within accepted fixture budget, no fabricated links | operator-gated | No committed budget yet |
| `graph_query_plan_regression_count` | graph | **Zero** accepted regressions; every known-bad plan signature fails the gate | hermetic gate | `go/internal/queryplan` validator, runs in CI |

Three of these are absolute and independent of any baseline: the two
zero-tolerance queue counts and the zero-regression plan count. The queue pair
is reinforced by the `queue_terminal_state` rule, which requires pending,
in-flight, retrying, failed, and dead-letter counts to all be zero for an
accepted terminal proof. The plan count is the one metric on this page already
enforced hermetically today.

### Why six rows have no committed baseline

`fact_rows_per_second`, `queue_claim_latency_p95_ms`, `reducer_drain_seconds`,
`graph_write_p95_ms`, `memory_high_water_mb`, and
`correlation_fanout_candidates_p95` carry only the relative rule, because no
accepted `scale-benchmark-artifact` run is committed to this repository. The
per-route and per-tool budgets behind `api_p95_ms` and `mcp_p95_ms` live in
`specs/capability-matrix.v1.yaml` rather than here, and are not scale-lab
numbers.

Two of the six deserve a specific caution, because in each case a
nearby-looking committed number is a different quantity.

`fact_rows_per_second` is computed by the #3171 producer as
`ingestion.fact_rows / ingestion.elapsed_seconds`. The gold points below record
*projection* and *parse* wall-clock, not ingestion elapsed time, and the
collector envelope treats those as separate concurrent stages. Dividing the
fact count by the 1,245 s projection duration therefore yields a
projection-throughput figure, not this metric — comparing a future ingestion run
against it would pass or fail on graph-projection speed instead.

`reducer_drain_seconds` is not the 1,245 s figure either. That is
`eshu-bootstrap-index` finishing its own source-local projection; `eshu-reducer`
drains the intents that projection enqueued only *after* bootstrap-index exits.
Its drain is governed by the Projection-Tail Backlog Target in
[Reducer claim-latency gate](reducer-claim-latency-gate.md), and that contract
is **stricter** than this metric's spec threshold: the spec allows "within 10
percent or 60 seconds, whichever is larger", while the projection-tail band
stops a run that is *either* more than 10 percent *or* more than 60 seconds
worse. On a short baseline those differ sharply — a 10-second baseline drained
in 65 seconds passes the spec bar and fails the projection-tail band. Where both
apply, the projection-tail contract governs.

Absolute per-slot targets become publishable the first time the benchmark
harness tracked by #3171 produces and commits an accepted artifact. That is an
operator run of machinery the repository already has — but only for the nine
metrics its artifact contract currently requires
(`scripts/run-scale-benchmark-artifact.sh`). `correlation_fanout_candidates_p95`
and `graph_query_plan_regression_count` are not among them, so an accepted
artifact can exist without either; publishing a fanout target needs that
contract extended first.

Note for contributors: `specs/scale-benchmark-artifact.sample.json` contains
metric values that look measured. It is a schema fixture — its `run.id` is
`scale-bench-sample` and its `run.commit` is all zeroes. Never cite it as
evidence.

## Named baselines

These are the measured, committed wall-clock numbers for the `git` collector's
Tier 4 full-corpus gold point: 896 repositories, 3,501,443 `fact_records`, on
PostgreSQL 18 with NornicDB. They are per-stage context, not a baseline for any
row in the table above — see the caution about `fact_rows_per_second` for why
dividing one by the other does not produce that metric.

| Phase | Wall-clock | Source |
| --- | --- | --- |
| Bootstrap projection complete | 1,245 s | [Collector performance envelope](collector-performance-envelope.md) |
| Deferred relationship backfill | 882 s | [Collector performance envelope](collector-performance-envelope.md) |
| Parse stage total | ~675 s | [Collector performance envelope](collector-performance-envelope.md) |

There is deliberately no single end-to-end figure here, and the rows above must
not be summed. The stages pipeline — the projector runs concurrently with
collection and the backfill — so their sum is not wall-clock time for any run.

An internal architecture review does refer to a "<15-minute 896-repo end-to-end"
pipelined known-good baseline. It is excluded here on purpose: it is not traced
to a committed run artifact, and
[Collector performance envelope](collector-performance-envelope.md) likewise
declines to publish it, presenting measured phases instead. Treat it as a
working expectation, not a published target.

## Budgets published on other surfaces

These are real, enforced budgets, but they belong to their own surfaces and are
not scale-lab metrics. They are listed here so this page is a complete index,
and linked rather than copied — each number is bound by a lockstep test to
exactly one document, and a second copy would drift silently.

- [Local performance envelope](local-performance-envelope.md) — cold start, warm
  start, query p95, dead-code, and bulk-write budgets per local profile.
- [Reducer claim-latency gate](reducer-claim-latency-gate.md) — the claim-latency
  budget and the per-handler ns/op ceilings.
- [Hybrid retrieval production gate](hybrid-retrieval-production-gate.md) —
  recall, precision, nDCG, latency, and vector-coverage bars.
- [Demo time-to-first-answer benchmark](local-testing/demo-ttfa-benchmark.md) —
  warm and cold TTFA targets for the demo path.
- [First five minutes benchmark](local-testing/first-five-minutes-benchmark.md) —
  the first-run envelope and its scorecard.

The local-envelope and hybrid-retrieval latency bars are not `api_p95_ms` or
`mcp_p95_ms` targets. They measure different surfaces under different profiles,
and treating them as scale-lab targets would misreport both.
