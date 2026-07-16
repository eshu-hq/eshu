---
name: eshu-performance-rigor
description: |
  Use for Eshu performance investigations, benchmarks, query or index
  optimization, queue/reducer throughput changes, cold-bootstrap timing,
  scaled replays, full-corpus proofs, and before/after performance claims.
  Enforces a staged proof ladder, comparable run manifests, correctness and
  concurrency proof, remote preflight, and honest end-to-end classification.
---

# Eshu Performance Rigor

Use this skill for any change whose acceptance depends on latency, throughput,
resource cost, queue drain, bootstrap duration, or a performance target. Add
`eshu-diagnostic-rigor` when the bottleneck is not yet localized. Add the
storage, concurrency, Go, Cypher, correlation, or golden-corpus skills required
by the touched surface.

## Non-Negotiable Contract

Accuracy comes first, performance second, and concurrency third. A faster
wrong answer, unsafe claim path, incomplete drain, hidden fallback, or
serialized workaround is a failure.

Before implementation, write down:

- the primary metric and its exact start and terminal events;
- the correctness invariant or intended behavior delta;
- the baseline run and known-normal band;
- the expected cardinality and worst-case partition;
- the minimum worthwhile improvement and stop threshold;
- the proof ladder and the conditions that permit escalation;
- the operator signal that will identify the same path in production.

Do not dispatch implementation or run the full corpus until the cheapest
representative shim proves the theory.

## Target Contribution Budget

Before prioritizing or dispatching a candidate, calculate and record:

- `current_total_seconds`: the accepted baseline's primary metric;
- `target_seconds` and `required_saving_seconds` (`max(current - target, 0)`);
- the candidate stage and its measured seconds;
- `maximum_recoverable_seconds`: the theoretical ceiling for that stage;
- `expected_saving_seconds`: the realistic win supported by the shim;
- `minimum_worthwhile_saving_seconds`; and
- the expected margin above or below the required target gap.

Do not prioritize a candidate that cannot theoretically recover the target gap
when another measured critical-path candidate can. Do not spend an
operator-scale run on an expected saving below the worthwhile threshold. A
small optimization may still proceed for a separately stated SLO or resource
goal, but it must not be presented as the path to the end-to-end target.

## Open Issue Performance Triage

Before choosing an open performance issue, re-rank it against current evidence.
Issue titles, old severity, and stale long-pole claims are not priority data.
Use the latest accepted baseline, phase timings, target gap, and correctness
blockers as the source of truth.

Classify each candidate before implementation:

- `strategic-target-work`: its measured or theoretical ceiling can materially
  close the active target gap.
- `hygiene-cleanup`: the issue is real and worth doing, but its expected saving
  cannot materially move the active wall-clock target.
- `diagnostic-only`: the work improves attribution or operator evidence without
  claiming a speedup.
- `correctness-blocker`: graph, storage, queue, or query truth is suspect; fix
  this before publishing or celebrating speed.
- `superseded`: newer proof or a merged fix changed the bottleneck or eliminated
  the reason to implement the issue as written.

If the candidate is `hygiene-cleanup`, do not use the main performance cycle on
it while a measured critical-path candidate exists. If newer measurements
contradict the issue body, update the issue with the new classification and the
evidence before dispatching implementation.

## Performance Orchestration

Use subagents or role agents whenever the work can be safely split into bounded
read-only or mechanical lanes, such as live GitHub issue-state inspection,
route/caller inventory, old/new query-shape proof, test-surface discovery, and
verification-log review. Each subagent handoff must include the relevant
project skills, exact surface, proof task, and out-of-scope boundary.

Do not dispatch implementation subagents for unproven optimization theories.
First prove or reject the theory with the cheapest representative shim. Once
the proof plan and acceptance packet are clear, route bounded TDD
implementation to an execution-focused agent.

Escalate performance investigation, debugging, bottleneck attribution,
query-plan interpretation, and proof-priority judgment to the strongest
available diagnostic/performance model. For current Eshu planning, use 5.6 Sol
when available; if it is unavailable, use the nearest high-reasoning
performance/debug model and record that substitution in the handoff.

## Resource-Qualified Claims

An absolute wall-clock target is valid only for its named reference profile;
it is not a hardware-independent product guarantee. Before using a run to
accept or reject an absolute target, record a non-sensitive measured resource
envelope: CPU architecture and logical CPU count, physical memory bytes,
storage kind, operating-system class, and any container CPU or memory limits.
Set `absolute_target_applicable` explicitly in the manifest.

Also record a human-readable `machine_profile`: category, provider or vendor,
model, cloud instance type when applicable, and a display name such as
`AWS EC2 <instance type>, 128 GiB` or `MacBook Pro, 32 GiB`. This makes evidence
understandable to contributors, while the measured resource envelope remains
the authority for comparability.

The resource envelope must be comparable to the accepted reference profile for
`absolute_target_applicable` to be true. A free-form `hardware_class` label is
useful for routing but is not sufficient evidence by itself. Differences in
CPU generation, throttling, storage latency, memory pressure, virtualization,
or container limits can invalidate an absolute-duration comparison even when
the nominal memory size matches.

Contributor runs on smaller or different machines remain useful. They may
prove correctness and a same-machine relative before/after improvement when
both runs use the same resource envelope and workload. Classify those results
as non-comparable to the reference profile and do not report a missed absolute
target as an Eshu regression. Do not publish a minimum hardware recommendation
from a single machine; establish it from repeated measurements across named
resource classes.

Machine capacity is only the supply side. Capture the Compose process demand as
well: configured replicas, CPU limits, memory limits/reservations, and a
phase-tagged time series for every service. Summarize peak CPU, peak working-set
memory, memory percentage, block I/O, restart count, and OOM state per service,
plus host memory pressure, load, swap, and I/O wait. A final `docker stats`
snapshot is not sufficient; it can miss the peak and cannot attribute pressure
to a pipeline phase.

Configured service limits are inputs and must match for a wall-clock comparison.
Observed service usage is an outcome: report its before/after delta, but do not
require it to be identical because reducing resource demand may be the intended
win. Missing usage evidence makes capacity/efficiency conclusions incomplete,
even when otherwise comparable wall-clock evidence remains usable with a caveat.

## Cost-Aware Diagnostic Dispatch

Reserve the strongest diagnostic model for bottleneck localization, hypothesis
design, profile/query-plan interpretation, and proof judgment. Once the theory
and implementation packet are complete, stop that diagnostic task. Use an
execution-focused model for bounded TDD implementation, and use scripts or the
coordinator for builds, routine polling, CI watching, GitHub bookkeeping, and
cleanup. Do not spend frontier reasoning tokens babysitting a long run.

## Skill Routing

- `eshu-diagnostic-rigor`: instrumentation, attribution, and unknown bottlenecks.
- `eshu-postgres-rigor`: SQL, indexes, DDL, transactions, locks, or Postgres plans.
- `cypher-query-rigor`: Cypher, graph indexes, graph writes, or backend dialect.
- `concurrency-deadlock-rigor`: workers, claims, leases, retries, locks, or ordering.
- `golang-engineering`: Go production code and tests.
- `eshu-golden-corpus-rigor`: result shapes, reducer graph truth, cassettes, or B-7/B-12.
- `eshu-code-review`: the final reviewed diff before every push and readiness claim.

## Proof Ladder

Every rung must preserve the same input semantics and record its result before
the next rung begins.

1. **Theory shim.** Use `EXPLAIN (ANALYZE, BUFFERS)`, Cypher `PROFILE`, a
   microbenchmark, or a throwaway query against representative worst-case data.
2. **Exactness.** For output-preserving work, prove bidirectional set difference
   0/0, identical ordered output, or equivalent counts. For a behavior fix,
   prove the explicit expected delta.
3. **Concurrency.** For claims, locks, leases, queues, DDL, or shared writers,
   prove contention, retry, idempotency, ordering, and failure recovery. Set
   equivalence alone is insufficient. Index and constraint candidates must
   prove first application, identical reapplication, restart/bootstrap
   behavior, and rollback on an isolated populated store. A fast first build
   does not authorize production DDL when repeated application mutates backend
   index state.
4. **Built-binary bounded replay.** Rebuild the production binary and run the
   worst-case repository, partition, scope, or backlog. Query-shape proof does
   not establish wall time.
5. **Small corpus.** Run the credential-free 20-25 repository or equivalent
   bounded corpus and verify graph/content/API truth.
6. **Full corpus.** Run once only after the previous rungs pass and the remote
   preflight matches a named baseline profile.

If a rung disproves the hypothesis, record it in the hypothesis ledger and do
not implement it. A rejected hypothesis is a valid result.

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

## Caller And Route Inventory

Before changing an index, readiness gate, cache, fallback, queue fence, or
shared state, inventory every caller and user-visible route. Search interfaces,
direct calls, indirect enrichment paths, pagination helpers, CLI, API, MCP,
background jobs, and recovery paths.

For each path state whether it:

- remains available;
- fails closed with a documented bounded error;
- uses a different exact index or scope;
- retries safely; or
- is intentionally outside the change.

Add tests for every distinct path class. Do not rely on final hostile review to
discover bypasses.

## Remote Preflight

Before reading recently merged skills, evidence, or code, refresh Git truth:

```bash
git fetch origin
git merge-base --is-ancestor <merge-commit> origin/main
```

Read from refreshed `origin/main`, the merge commit, or the dedicated worktree.
Do not treat a stale local main checkout as merged truth.

Before a scaled or full-corpus run, capture and compare:

- local and remote Eshu commit, stable patch ID when rebased, and clean state;
- graph backend commit, image digest or immutable image ID;
- Compose files, service topology, and owner process count;
- corpus identity and repository count;
- clean-volume versus preserved-volume state;
- schema/bootstrap state;
- effective worker, queue, timeout, connection-pool, and memory knobs;
- pprof and resource sampling state;
- the measured resource envelope and whether the reference-profile target is
  applicable;
- configured Compose service limits and phase-tagged per-service resource
  sampling;
- controller terminal condition and expected minimum runtime.

Bind evidence to the exact source input hash, built binary or image digest,
proof corpus, and harness or browser-runner hash. A stale image or runner labeled
as the final proof is a correctness defect in the evidence, not a bookkeeping
detail.

Remote source must come from Git: push the reviewed feature branch, then fetch
and check out or fast-forward it on the remote machine. Do not use `rsync` or
copy an unreviewed worktree. Keep hosts, users, IPs, key paths, and remote
checkout paths in user-local configuration, never in this repository.

Stop the run before launch if the intended topology or profile differs from the
baseline without an explicit experimental reason.

## Milestones And Terminal Truth

Capture these as separate timestamps and elapsed seconds where applicable:

- launch;
- schema/bootstrap ready;
- collection complete;
- source-local projection complete;
- queue terminal;
- shared materialization complete;
- vector/search readiness complete;
- post-drain finalizers complete;
- bootstrap process exit;
- API ready;
- MCP ready;
- first representative query success.

Never compare queue terminal with process exit or query readiness as if they
were the same metric. The primary terminal event must be identical between old
and new runs.

Report every duration as exact seconds plus a human-readable value, for example
`1205.924s (20m05.924s)`.

## Run Manifest

Every scaled or remote run used as evidence must produce a machine-readable run
manifest following [references/run-manifest.md](references/run-manifest.md).
The manifest records identity, topology, workload, milestones, truth counts,
resource evidence, readback, cleanup, and caveats.

The detailed manifest is operator-local and must not be committed. If the PR
needs a committed public aggregate, render it through
`specs/scale-benchmark-artifact.v1.yaml` and validate it with
`scripts/verify-scale-benchmark-artifact.sh`; do not invent a second public
artifact schema.

Public evidence may include only a run basename. Never publish a hostname, IP,
user, private key path, workstation path, remote checkout path, credential, or
secret-bearing DSN.

## Baseline Promotion

Keep one named accepted manifest for each proof profile in operator-local state.
Promote a replacement only when:

- the tested source is clean and its exact/equivalent commit is recorded;
- the primary metric boundaries and topology are explicit;
- the queue is non-empty, fully succeeded, and has zero failed/dead-letter work;
- required scope/readiness truth is terminal; and
- API, MCP, index status, and representative reads pass.

An accepted baseline may honestly classify the performance target as missed; it
is the current truthful comparison point, not a claim of success. Retain the
prior entry until promotion succeeds. Artifact-backed terminal counts override
earlier controller summaries when post-drain work reopens the queue.

## Comparability Gate

Before calculating a speedup, verify that old and new agree on:

- primary start and terminal events;
- corpus identity and cardinality;
- Eshu behavior profile and backend build;
- topology and service ownership;
- worker and connection knobs;
- clean or warm storage state;
- hardware class and the measured resource envelope;
- configured Compose replicas and resource limits;
- absolute-target applicability; and
- correctness/terminal counts.

If any load-bearing field differs, label the total non-comparable. Compare only
the matching phase or rerun the baseline. Never hide setup time inside one side
of a comparison.

## Stop Conditions

- Stop and profile when a healthy run regresses by more than 10% or 60 seconds.
- Stop a remote run at the declared time box unless it is making bounded,
  observable progress toward an operator-scale terminal proof.
- Do not launch with a time box shorter than the measured inherent cold-start
  floor; state the expected duration before launch.
- Do not merge a local-path win as an end-to-end target win when the target was
  missed.
- Run the full corpus at most once per proven candidate unless a documented
  comparability or proof failure requires a rerun.

## Retention Modes

Declare one mode in the run manifest before closeout:

- `stop-and-preserve`: stop readers, workers, controllers, and containers while
  retaining labeled data for likely review follow-up.
- `keep-live`: retain an interactive stack only when the user explicitly asks.
- `destroy`: remove the proof's labeled containers, volumes, networks,
  controllers, and temporary credentials after merge or final disposition.

Use a unique issue/run Compose project label and act only on that label. Never
use broad Docker pruning. For expensive proofs, `stop-and-preserve` avoids a
needless rerun while review is active; promotion or preservation does not waive
eventual cleanup.

## Evidence Carry-Forward After Rebase

An expensive remote result may carry forward across a base-only rebase only
when the old and new commits have identical stable patch IDs and the incoming
base diff does not touch the measured runtime, schema, topology, fixtures, or
proof harness. Record both commits and the patch ID. This does not waive
`make pre-pr`, the mandatory final `eshu-code-review`, or targeted local proof
on the rebased head.

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

## Required Closeout

Before push or merge-readiness:

1. Run the focused reproduction and appropriate local integration/golden gates.
2. Run `scripts/test-verify-performance-evidence.sh` and
   `scripts/verify-performance-evidence.sh` when not already selected.
3. Run a preliminary full `eshu-code-review`; any P0/P1/P2 finding must be
   fixed and re-proven before proceeding.
4. Only after that review is clean, run `make pre-pr` once, immediately before
   the intended push.
5. Run the final full `eshu-code-review` on the exact post-preflight diff. Any
   diff change invalidates the verdict and repeats this sequence.
6. Capture live CI and review-thread truth after push.
7. Update the issue and PR with the hypothesis ledger, manifest-derived proof,
   exact and human durations, target result, and next long pole.
8. Apply the manifest's declared retention mode, verify its resulting resource
   state, and destroy retained proof resources after merge/final disposition.
