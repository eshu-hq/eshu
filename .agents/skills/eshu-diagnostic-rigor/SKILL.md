---
name: eshu-diagnostic-rigor
description: Establish causes of Eshu runtime failures, queue stalls, backend slowness, or intermittent proof failures before changing behavior.
---

# Eshu Diagnostic Rigor

This skill owns causal diagnosis and observability. Use
[eshu-performance-rigor](../eshu-performance-rigor/SKILL.md) for optimization,
benchmark design, or any latency, throughput, resource, or wall-time claim.
Ordinary passing test runs do not require this diagnostic workflow.

## Establish The Cause

Read the relevant runtime contracts and active ADR before changing behavior.
Start from the failing run's logs, state, and artifacts. Attach each causal claim
to an observation; label unproven causes as hypotheses whenever repeated. Give
an independent investigator the symptom and raw evidence, not a cause to confirm.
For intermittent behavior, report a measured occurrence rate rather than treating
one passing sample as proof.

Form a narrow hypothesis and add telemetry if existing signals cannot test it.
Use focused local proof; rebuild production binaries before runtime validation.
Preserve correctness, bounded runtime cost, and intended concurrent behavior.
A performance-sensitive change needs the impact declaration and applicable proof
from [eshu-performance-rigor](../eshu-performance-rigor/SKILL.md) before
implementation; this skill does not define a second proof ladder.

Bind proof to exact source, binary/image digest, retained-data and harness/browser
runner identity. A stale artifact invalidates a final claim. Never source Compose
env files as shell programs; pass required variables explicitly without leaking
secrets into logs or tracked evidence.

## Read The Relevant Procedure

- For reducer, queue, projector, or backend slowness, read
  [runtime-attribution.md](references/runtime-attribution.md). Separate waiting,
  handler work, graph writes, fact loads, conflict/readiness blocking, host
  pressure, ambient backend work, and stale setup before tuning.
- For dashboard/API/MCP failures, read
  [api-mcp-validation.md](references/api-mcp-validation.md). Scope first, require
  limits/timeouts and explicit truncation, inspect truth envelopes, and prove
  response ownership. Do not repeat an unbounded slow or hung call.
- For intermittent tests or live-gate failures, read
  [gate-contention.md](references/gate-contention.md). Check resource contention
  first and serialize heavy gates across the shared host without killing peers.
- For remote/scaled diagnostics, read the performance skill's
  [scaled-run procedure](../eshu-performance-rigor/references/scaled-runs.md)
  and [run manifest](../eshu-performance-rigor/references/run-manifest.md).
  Enable pprof and capture the effective container environment before
  interpreting slowness; machine-specific connection details stay operator-local.

Use Go, Postgres, Cypher, concurrency, or correlation skills for those changed
surfaces. Worker/lease/claim rewrites require independent concurrency proof;
row-set equivalence does not establish lock or lease safety.

## Record The Result

State the established cause, evidence, remaining uncertainty, and next action.
Distinguish diagnostic visibility, correctness, handler work, scheduling, and
end-to-end improvements; a diagnostic result need not claim a speedup. Record
rejected hypotheses to avoid repeating them.

For runtime evidence updates and hot-path PRs, read
[performance evidence](../eshu-performance-rigor/references/evidence.md) for
tracked markers, commands, and ADR fields. Root-cause notes carry
`Root-Cause Evidence:` naming the establishing observation. A gate verifies that
evidence exists, not that the causal reasoning is sound. Follow the root review
and promotion workflow when implementing the diagnosed fix.
