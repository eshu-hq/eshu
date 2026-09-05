# Performance Evidence And Classification

## Hypothesis Ledger

Keep a durable table in the issue, ADR, or package evidence note:

| candidate | stage seconds | expected saving | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | ---: | ---: | --- | ---: | ---: | --- | --- | --- |

Use these dispositions: `proven`, `rejected`, `diagnostic-only`, `blocked`, or
`superseded`. Record no-win experiments so another agent does not repeat them.

## PR And Issue Evidence Table

Every performance PR description and closeout issue comment must include a
before/after table. Keep it small but explicit:

| Metric | Before | After | Delta | Evidence |
| --- | ---: | ---: | ---: | --- |
| Primary metric or shim | | | | |
| Correctness diff | | | | |
| Target contribution | | | | |
| Next long pole | | | | |

For rejected or hygiene candidates, fill the same table with the measured
ceiling and disposition. Do not hide a tiny but real win behind vague language;
state that it is hygiene when it cannot close the target gap.

## Final Classification

Classify each result as one or more of:

- `Rejected hypothesis`
- `Diagnostic win`
- `Correctness win`
- `Handler win`
- `Scheduling win`
- `Phase wall-clock win`
- `End-to-end wall-clock win`
- `Target achieved`
- `Target missed`

Always name the next measured long pole. Do not claim the overall target when
only a component improved.

Report first-request cold-client latency separately from warm steady-state
latency. Warm p50/p95 numbers do not prove a cold-start SLO. Compare each class
to the checked-in capability SLO, and attribute remaining time among graph,
Postgres, LLM, duplicate requests, transport, and browser rendering before
proposing a cache. Redis or another cache is a candidate only after attribution
shows repeated cacheable work dominates and an invalidation/exactness contract
is proven.

## Concrete Repo Gates

Before finishing any hot-path runtime PR, run:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```

The gate fails when changed Go files introduce or modify Cypher, graph writes,
worker claims, leases, batching, goroutines, channels, queue behavior, or
runtime stages without a tracked docs/ADR/package note containing:

- `Performance Evidence:`, `Benchmark Evidence:`, or
  `No-Regression Evidence:`
- `Observability Evidence:` or `No-Observability-Change:`

The note must name the measurement, backend/version, input shape, queue or row
counts, and the metrics/spans/logs/status fields that let an operator diagnose
the path. PR text alone is not durable evidence.

## ADR Evidence

Update the active ADR with:

- commit id
- run id
- corpus size and terminal state
- wall time before and after
- repository size signals, indexed file count, and fact count
- key stage sums/maxima
- CPU idle, IO wait, and disk idle
- classification and next action

Record no-win experiments. They are valuable because they prevent repeated
false leads.
