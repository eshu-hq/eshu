---
name: eshu-correlation-truth
description: Use when changing Eshu workload admission, deployable-unit correlation, materialization, deployment tracing, service/repository story synthesis, or query truth where positive, negative, ambiguous, graph, and API evidence must all agree before the change is considered correct.
---

# Eshu Correlation Truth

## Overview

Use this skill for reducer and query work where evidence becomes canonical graph
truth. The goal is not "tests pass"; the goal is "the graph tells the truth and
the query surfaces say the same thing."

## When to Use

- Reducer changes in `go/internal/reducer`
- Query truth changes in `go/internal/query`
- Graph write or relationship projection changes
- Workload classification, admission, or materialization changes
- Deployment tracing, service story, or repo story changes
- Correlation fixture or compose verification changes

Do not use this skill for ordinary CRUD or isolated UI work.

## Required Thinking Order

1. MUST trace the full path: raw evidence -> candidate -> admission ->
   materialization -> graph write -> query surface.
2. MUST name the invariants before editing. Example: "deployment repos must remain
   provenance-only" or "controller-only repos must not materialize as services."
3. MUST list the edge classes that could falsify the change before writing code.

## Mandatory Proof Matrix

Every non-trivial correlation change MUST cover all of these:

- Positive case: the intended service or deployment story materializes.
- Negative case: provenance-only or utility evidence stays non-materialized.
- Ambiguous case: mixed signals do not over-admit or invent truth.
- Graph proof: inspect the canonical nodes and edges directly.
- Query proof: confirm repo, service, and deployment surfaces agree with the
  graph.

Minimum repo families to think through:

- Service repo with strong deployment evidence
- Deployment or controller repo that must stay utility or provenance-only
- Controller-backed service repo such as Jenkins or GitHub Actions plus image
- Ambiguous multi-unit or multi-Dockerfile repo
- Repo with namespace-like strings that are not real environments

## Workflow

### 1. Read the local docs first

Read:

- `docs/docs/reference/local-testing.md`
- `docs/docs/deployment/docker-compose.md` when compose is involved
- `docs/docs/reference/telemetry/index.md` when runtime observability changes

### 2. Write the failing proof first

Add or update focused tests before implementation. Prefer targeted reducer or
query tests that show the exact false positive, false negative, or ambiguity.

### 3. Implement the smallest truthful fix

Do not patch query output to hide graph problems. Fix the actual stage that is
wrong:

- extraction
- correlation
- admission
- materialization
- graph projection
- query synthesis

### 4. Verify in layers

Run focused package tests first, then a fresh compose verification after the
last logic patch. A stale container or stale reducer run is not valid evidence.

Required layers:

- Focused Go tests for touched packages
- Fresh compose rebuild/restart verification when fixture truth changed
- Direct graph check of canonical nodes and edges
- API/query checks for repo context, service context, and deployment trace
- Performance and observability markers when the change touches hot-path graph,
  reducer, queue, or materialization code. Run
  `scripts/verify-performance-evidence.sh` and include
  `Performance Evidence:`, `Benchmark Evidence:`, or
  `No-Regression Evidence:` plus `Observability Evidence:` or
  `No-Observability-Change:` in a tracked repo file.

### 5. Reconcile disagreements

If tests pass but the graph is wrong, the change is not done.
If the graph is right but the query surface lies, the change is not done.
If repo context and service context disagree, the change is not done.

## Direct Graph Truth Checks

Use direct graph inspection whenever correlation truth is in question. Confirm:

- which repositories define workloads
- which workloads materialize instances
- which deployment relationships exist
- which environments and platforms are actually present

Do not infer truth from logs, confidence scores, or reducer completion alone.

## Anti-Patterns

- Treating reducer completion timing as proof that the logic is correct
- Inventing default environments, platforms, or instances without evidence
- Letting broad provenance like raw `k8s_resource` or raw `argocd_application`
  over-admit workloads
- Validating only one happy-path service repo
- Accepting repo-name or namespace coincidence as deployment truth
- Fixing API output while leaving the graph semantically wrong

## Done Criteria

The change is done only when:

- tests prove the positive, negative, and ambiguous cases
- direct graph inspection matches the intended model
- repo context, service context, and deployment trace tell the same story
- you can explain why adjacent repo families did not regress
