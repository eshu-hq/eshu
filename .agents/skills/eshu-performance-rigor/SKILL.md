---
name: eshu-performance-rigor
description: Prove Eshu latency, throughput, resource, or wall-time changes with representative correctness and concurrency checks and comparable measurements.
---

# Eshu Performance Rigor

This skill owns measured performance impact. Use
[eshu-diagnostic-rigor](../eshu-diagnostic-rigor/SKILL.md) first when the cause or
bottleneck is unknown; routine test execution alone does not need diagnosis.

## Essential Contract

Accuracy comes first, performance second, and concurrency third. A faster wrong
answer, unsafe claim, incomplete drain, hidden fallback, or serialized workaround
is a failure. Do not raise worker defaults without safe conflict-domain and
backend-headroom evidence.

Before implementation, record the stage, exact metric start/terminal events,
correctness invariant or intended delta, expected cardinality and worst-case
partition, baseline/known-normal band, minimum worthwhile improvement, stop
threshold, required proof, and production diagnostic signal. Prove the theory
with the cheapest representative shim before implementing or dispatching it.

Read [proof-plan.md](references/proof-plan.md) to select the acceptance-driven
proof ladder: theory, exactness or intended delta, applicable concurrency proof,
and rebuilt-binary bounded replay. Escalate through smaller proofs to a full
corpus only when required by acceptance or the scope of the claim. Never waive
required evidence or infer production wall time from query shape alone.

Bind evidence to the exact source hash, binary/image digest, workload and harness
identity. Compare identical metric boundaries, data, backend, runtime knobs,
storage state, and resource profiles; label incompatible totals non-comparable.
Preserve graph/content/API truth and terminal counts. Serialize live gates and
benchmarks across the shared machine; do not overlap or terminate a peer's run.
Stop and profile a healthy regression greater than 10% or 60 seconds. Declare a
run time box that accommodates the measured cold-start floor.

## Target Contribution Budget

When selecting work for an end-to-end target, calculate
`required_saving_seconds = max(current_total_seconds - target_seconds, 0)`,
`maximum_recoverable_seconds` from the measured stage, and
`expected_saving_seconds` from cheap proof. Use
[target-triage.md](references/target-triage.md) for target margins, current issue
prioritization, and bounded delegation. A separately justified local SLO or
resource improvement need not close the overall target gap; report its scope.

## Resource-Qualified Claims

Absolute targets apply only to the accepted measured resource profile; record
`absolute_target_applicable`. Different contributor hardware may prove a
same-machine relative improvement, without proving the reference target.
For scaled/remote proof or capacity claims, read
[scaled-runs.md](references/scaled-runs.md) for resource envelopes, per-service
sampling, preflight, milestone boundaries, and comparison rules. Use the
operator-local [run manifest](references/run-manifest.md) for every scaled or
remote evidence run; never commit its private operational details.

Before relying on recently merged code or evidence, refresh Git and verify
ancestry with `git merge-base --is-ancestor <merge-commit> origin/main`; do not
use stale local main as merged truth. Remote source comes from the reviewed Git
branch. Apply the review/push gates before transporting it.

## Baseline Promotion

Promote a baseline only from clean identified inputs with a nonempty fully
succeeded queue, zero failed/dead-letter work, terminal readiness, and passing
API/MCP/index/representative reads. Keep the prior accepted manifest until
promotion succeeds. A truthful baseline may miss the target. The complete
promotion and base-only rebase carry-forward rules are in
[scaled-runs.md](references/scaled-runs.md).

## Retention Modes

Declare `stop-and-preserve`, user-requested `keep-live`, or `destroy` in the run
manifest. Act only on the unique issue/run Compose label; never broad-prune.
Verify cleanup and eventually destroy retained resources after final disposition.
Read [scaled-runs.md](references/scaled-runs.md) before launching these runs.

## Evidence And Closeout

Read [evidence.md](references/evidence.md) for durable evidence markers, the
hypothesis ledger, before/after reporting, ADR updates, and result classification.
Keep cold-client latency separate from warm p50/p95. Claim only the measured
level of improvement and name the next measured bottleneck.

Run focused reproduction and required integration/golden and performance-evidence
gates. Follow [eshu-code-review](../eshu-code-review/SKILL.md) and root rules for
review, attestation, `make pre-pr`, and push; a verified unchanged receipt avoids
a duplicate semantic review. Changed inputs invalidate that receipt. Capture
live CI/review truth and apply the declared retention mode at closeout.

Add storage, Cypher, Go, concurrency, correlation, or golden-corpus skills when
the touched contract needs them; their correctness requirements still apply.
