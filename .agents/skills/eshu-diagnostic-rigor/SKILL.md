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
- Design MCP/API calls to be bounded before running them: scope first, limit
  required, timeout expected, and truncation explicit.
- MUST rebuild binaries before runtime testing.
- MUST NOT start with the full corpus. Use one large repo, one small/medium proof,
  then the 20-25 repo corpus, then full corpus.
- MUST NOT increase worker defaults without evidence of safe conflict domains and
  backend headroom.
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

If CPU and disk are idle, suspect serialization, queue fences, query shape,
backend lookup/validation behavior, or data shape before adding workers.

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
7. Record repository size signals, indexed file count, fact count, backend, and
   commit id for every run used as performance evidence.
8. Classify the result in the ADR.

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
