---
name: eshu-diagnostic-rigor
description: |
  Use for Eshu runtime diagnostics, reducer throughput work,
  graph backend performance, local or CI proof runs, queue/shared projection
  analysis, or ADR evidence updates. This is the default Eshu project skill for
  performance and correctness investigations; add Go, Cypher, concurrency, or
  correlation skills only when editing those specific surfaces.
---

# Eshu Diagnostic Rigor

Use this skill before Eshu reducer, ingester, projector, queue, graph backend,
NornicDB, or runtime performance work.

## Skill Routing

Start with this skill for Eshu diagnostic work. Add other skills only when the
change needs their domain:

- `golang-engineering`: Go code edits or Go tests.
- `cypher-query-rigor`: graph query, write shape, indexes, or backend dialect.
- `concurrency-deadlock-rigor`: workers, leases, retries, conflict keys, or queue ordering.
- `eshu-correlation-truth`: correlation, materialization truth, or query truth.
- `skill-creator`: creating or updating skills.

## Operating Rules

- MUST read repo docs and the active ADR before changing runtime behavior.
- MUST preserve correctness before performance. A fast wrong graph is a failure.
- MUST NOT introduce unmeasured performance regressions. New capability cost is
  acceptable only when documented, bounded, and justified by correctness.
- MUST write a performance impact declaration before implementation for
  collectors, parsers, reducers, projectors, graph writes, queues, workers,
  runtime Compose/Helm settings, NornicDB defaults, and graph-backed API/MCP
  calls. Name the affected stage, expected cardinality, baseline or
  known-normal band, proof ladder, and stop threshold.
- Design MCP/API calls to be bounded before running them: scope first, limit
  required, timeout expected, and truncation explicit.
- MUST rebuild binaries before runtime testing.
- MUST NOT start with the full corpus. Use one large repo, one small/medium proof,
  then the 20-25 repo corpus, then full corpus.
- MUST NOT increase worker defaults without evidence of safe conflict domains and
  backend headroom.
- For remote or full-corpus proof, enable pprof and capture the effective
  runtime environment from the containers before interpreting slowness.
- Keep machine-specific hostnames, keys, paths, and IPs out of repo docs.

## Diagnostic Model

Separate these before proposing an optimization:

- queue wait
- handler duration
- actual graph/backend write time
- fact/input load time
- shared projection wait and processing time
- conflict blocking or readiness wait
- CPU idle, IO wait, and disk idle
- ambient backend work such as embeddings, background indexing, or non-Eshu
  runtime features
- stale image, wrong branch, missing schema/bootstrap, or mismatched backend
  build

If CPU and disk are idle, suspect serialization, queue fences, query shape,
backend lookup/validation behavior, or data shape before adding workers.

Timeout-shaped failures are only evidence, not diagnosis. Classify the failure
as timeout budget, query shape, missing schema/index, backend fallback,
transaction validation, retry/idempotency behavior, stale image, or ambient
backend work before patching.

## MCP/API Call Checklist

Before calling or designing an Eshu MCP/API tool:

- resolve the smallest canonical scope first (`repo_id`, `workload_id`,
  `service_id`, or `environment`)
- prefer cheap summary/count/handle calls before payload-heavy drilldowns
- confirm local MCP owner ports are current when running against a local Eshu
  service
- inspect the Eshu envelope (`truth.level`, `truth.profile`,
  `truth.freshness.state`, and `error`) before interpreting results
- classify slowness as transport, stale owner ports, backend health, query
  shape, payload size, or runtime-mode selection before retrying

Do not repeat the same unbounded call after a slow or hung attempt.

## Evidence Ladder

For each runtime slice:

1. Form a narrow hypothesis.
2. Add telemetry first when the current signal cannot prove or disprove it.
3. Run focused local tests.
4. Rebuild binaries.
5. Run the small proof ladder before any full corpus run.
6. Capture wall time, terminal queue state, shared projection completion, CPU
   idle, IO wait, disk idle, and relevant handler/stage sums.
7. For full-corpus or remote proof, report collector stream complete,
   projection/bootstrap complete, and queue-zero as separate timings. Also
   record queue counts, retrying, dead letters, Eshu commit, NornicDB commit or
   image tag, clean-volume state, schema/bootstrap state, pprof state, and
   effective container runtime knobs.
8. If a run is healthy but slower than the known-normal band by more than about
   10% or 60 seconds, stop and profile before merge.
9. Record repository size signals, indexed file count, fact count, backend, and
   commit id for every run used as performance evidence.
10. Classify the result in the ADR.

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

## Result Classification

Every change must be labeled honestly:

- `Diagnostic win`: improves visibility but not necessarily wall time.
- `Correctness win`: fixes truth, readiness, or completion behavior.
- `Handler win`: reduces measured handler work.
- `Wall-clock win`: reduces end-to-end proof wall time.
- `Scheduling win`: reduces queue wait without moving wall time.
- `Rejected hypothesis`: measured no material improvement; revert if the code
  change is not otherwise needed.

Do not present handler-only wins as throughput wins unless wall-clock evidence
supports it.

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

## Common Eshu Reducer Lessons

- Queue wait alone is not proof that more concurrency helps.
- Broad conflict keys may be correct and still not be the current bottleneck.
- Full fact loads can dominate handlers even when graph writes are cheap.
- Shared projection wall time must be split into wait, selection, lease claim,
  processing, graph write, and completion/ack before tuning workers.
- NornicDB performance depends on exact Cypher shape, label/index lookup,
  relationship-existence checks, transaction validation, and commit behavior.
