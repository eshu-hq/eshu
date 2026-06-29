---
name: eshu-code-review
description: Use when reviewing Eshu diffs, PRs, self-reviews, pre-push work, merge-readiness, graph/Cypher changes, runtime recovery changes, cassette/golden updates, generated artifacts, or performance evidence.
---

# Eshu Code Review

Eshu reviews are proof reviews. Start from reject. Author intent, local memory,
and "this is just docs" are not evidence. Approve only when the final diff,
required proof tier, and review findings all agree.

Core rule: review the work product, not the story of the work. A separate
reviewer should receive the same evidence packet and reach the same verdict. In
self-review mode, rebuild that separation by reading only the final diff,
requirements, comments, and verification output before judging.

## Required Background

Load the project skills that match the diff. Compose them; do not duplicate or
water them down:

- `golang-engineering` for Go code, Go tests, package docs, or Go review.
- `cypher-query-rigor` for Cypher, graph schema, graph query/write, indexes, or
  backend dialect work.
- `eshu-correlation-truth` for projected graph/query/API/MCP truth.
- `eshu-diagnostic-rigor` for runtime, reducer, queue, performance, or proof
  evidence.
- `eshu-golden-corpus-rigor` for cassettes, golden snapshots, replay gates, or
  query/MCP response shapes.
- `eshu-mcp-call-rigor` for API/MCP tool contracts or graph-backed query calls.
- `concurrency-deadlock-rigor` for workers, leases, retries, queue ordering,
  batching, conflict keys, or shared state.
- `telemetry-coverage-discipline` when telemetry, metrics, spans, logs,
  dashboards, or coverage docs are touched.
- `generator-script-discipline` when generators or generated artifacts are
  touched.
- `eshu-folder-doc-keeper` when package README.md, doc.go, or scoped AGENTS.md
  files are touched.

## When To Use

Use this before every Eshu `git push`, PR create/update, and merge-readiness
claim. Use it for self-review when no separate reviewer is available. Re-run it
after any review fix that changes the diff or verification evidence.

Inputs required:

- final diff against the intended base;
- base SHA, head SHA, branch, PR or push target, and whether main moved;
- acceptance criteria, PR context, and review comments;
- files changed, including generated artifacts;
- commands and runtime proof actually run;
- current open review findings;
- pinned backend versions and current NornicDB source/docs when Cypher,
  graph-backed reads/writes, schema, or backend behavior is touched.

If any input is missing, the review verdict is `blocked`, not "looks good".

## Reviewer Stance

Review in rejection mode:

- Assume the diff has one correctness bug, one performance risk, and one
  workflow loophole until proven otherwise.
- Treat skipped proof as a finding unless the selected proof tier explains why
  it is out of scope.
- Treat "follow-up" as suspicious until the review proves the missing condition
  is outside the PR scope.
- Treat generated files, cassettes, snapshots, OpenAPI, capability inventory,
  and root agent files as contracts, not incidental outputs.
- Treat old review comments as unresolved until they are fixed in HEAD, resolved
  in the review-thread API, or proven obsolete by an explicit outdated thread.

Do not soften a finding because the change is small. Small process wording can
authorize large future mistakes.

## Proof Tier Decision

Select exactly one tier and explain why it is enough. If cassette proof is
sufficient, name the exact cassette/golden assertions that would fail on the
bug. If it is not sufficient for behavior changed by the PR, name the missing
runtime condition and block merge until the stronger gate runs. Link or create a
follow-up only when the stronger proof is genuinely outside the PR scope and the
current PR does not claim that condition is proven.

| Tier | Use when |
| --- | --- |
| Unit/static proof enough | Pure helper logic, parser-local behavior, generated string construction, or small contract code with no projected truth or runtime coupling. |
| Cassette/golden replay required and sufficient | Deterministic fact emission, reducer/projector truth, API/MCP response shape, capability truth, dead-code classification, cross-repo liveness, stale generations, tenant/repo scope boundaries, or no-provider-key evidence is covered by committed replay inputs and golden assertions. |
| Backend-required cassette/replay required | Correctness depends on real NornicDB/Neo4j behavior, Cypher dialect support, schema/index behavior, planner/hot-path eligibility, or exact emitted query shape against a live graph backend. |
| Scaled/performance replay required | Small replay may be correct but cardinality, fanout, queue depth, batching, graph write budgets, Postgres indexes, or p95/p99 latency can fail. |
| Full remote corpus required | Live collector behavior, clone/discover/parse cost, provider credentials, cross-service startup/restart behavior, image/runtime version drift, pprof/resource attribution, or queue-terminal guarantees are load-bearing. |

Wrong proof tier is a P1 unless it could ship wrong graph/query/deployment truth
or private data, in which case it is P0.

Pressure scenarios reviewers must distinguish:

- Dead-code semantics: cassette/golden replay is sufficient only when the
  library asserts live-by-consumer, unknown ownership, stale generations,
  cycles, tenant boundaries, API/MCP parity, evidence citations, confidence
  labels, and candidate bucket items.
- Graph write/retract timeout fixes: normal cassette truth is not enough;
  backend-required or scaled proof must expose graph-write timeout budgets.
- Reducer, materialization, or search-index long poles: replay can expose queue
  truth, but scaled or full-corpus proof is needed for latency and pprof.
- Parser regressions: collector cassettes are insufficient when they replay
  after collection or parse instead of exercising the broken parser path.
- Bootstrap or DDL restart waits: require fault-injection or live runtime
  restart proof rather than ordinary replay.
- Backend image or optimizer upgrades: cassette/golden replay proves functional
  truth, but backend-version, hot-path, startup, and performance proof need
  stronger validation.

## Pass 0: Scope, Ownership, And Diff Integrity

Before reviewing behavior, prove the review is pointed at the right work:

- base/head SHAs match the rebased final diff that will be pushed or merged;
- branch target is not `main` or `master`;
- touched surfaces map to their owning service or package boundary;
- scoped `AGENTS.md` rules and required skills have been loaded;
- changed files are limited to the intended issue/PR scope;
- no sibling PR rollback, unrelated deletion, generated-output churn, or
  accidental main-checkout mutation slipped in;
- root `AGENTS.md` and `CLAUDE.md` remain in lockstep when either changes;
- `.codex/skills` and `.claude/skills` discovery links exist for project
  skills that must be visible to both harnesses.

## Pass 1: Correctness And Truth

Review for wrong graph, query, API, MCP, or CLI truth before considering
performance. Check:

- missing tests or tests that do not exercise the production subject;
- raw evidence -> fact -> queue -> reducer/projector -> graph/content ->
  API/MCP agreement;
- fixture intent, cassettes, B-12 golden snapshot, and replay coverage;
- tenant/repo scope boundaries, stale generations, unknown/ambiguous ownership,
  cycles, duplicates, empty state, invalid input, no-provider-key behavior, and
  deterministic evidence preservation;
- cross-repo/live-if-used-by-consumer semantics and evidence citations;
- OpenAPI, HTTP, MCP, CLI, docs, and capability inventory lockstep.

## Pass 2: Performance And Storage/Query Shape

Review the same diff for cost and backend shape after correctness is understood.
Check:

- hot-path Cypher, graph writes/retracts, Postgres queries, indexes, and
  constraints;
- unbounded all-graph/all-table scans, late LIMIT, broad OR, function-wrapped
  indexed predicates, optional branch multiplication, missing deterministic
  ordering, and payload size;
- reducer/shared-projection queue pressure, graph write budgets, batching,
  worker knobs, and full-corpus or no-regression evidence;
- missing instrumentation or missing `Performance Evidence:`,
  `Benchmark Evidence:`, `No-Regression Evidence:`, `Observability Evidence:`,
  or `No-Observability-Change:` markers when required.

### NornicDB/Cypher Review

When Cypher, graph reads/writes, query-shape generation, reducer projection, or
API/MCP graph-backed responses change:

- Compare Eshu's pinned NornicDB image/tag/digest against current NornicDB
  docs/source before relying on optimizer behavior.
- Read Eshu `docs/public/reference/cypher-performance.md`,
  `docs/public/reference/nornicdb-pitfalls.md`,
  `docs/public/reference/nornicdb-tuning.md`, and the relevant current
  NornicDB source/docs such as `docs/performance/hot-path-query-cookbook.md`,
  `docs/skills/cypher-queries.skill.md`, `pkg/cypher/*hotpath*_test.go`, and
  `pkg/cypher/executor_hotpath_trace.go`.
- Identify the expected named fast path or deliberate fallback:
  `UnwindMergeChainBatch`, `UnwindMultiMatchCreateBatch`,
  `MergeSchemaLookupUsed`, `CompoundQueryFastPath`,
  `CallTailTraversalFastPath`, indexed traversal seed paths, or another traced
  flag from current source.
- Prove `MergeScanFallbackUsed=false` and `OuterScanFallbackUsed=false` for
  intended indexed paths unless fallback is deliberate, bounded, and measured.
- Require exact emitted query-shape tests or live profile/trace evidence for
  generated Cypher; simplified hand-written query tests are not enough.
- Verify every multi-label MATCH/MERGE alternative label has the required
  uniqueness constraint or property index. One unindexed alternative can flip
  `MergeScanFallbackUsed=true`.
- Prefer stable parameterized query templates. Whitespace/query-text churn can
  defeat plan-cache reuse.
- Review DDL/bootstrap separately: schema DDL must be startup-first,
  idempotent, and not reissued against populated stores in a way that blocks
  restarts behind corpus reads.

## Pass 3: Reliability, Concurrency, Security, Workflow Hygiene

Review for production operation and delivery safety:

- retries, leases, lock order/duration, transaction scope, idempotency,
  duplicate delivery, partial failure, rollback, recovery, and dead letters;
- startup/restart lock waits, schema/bootstrap behavior, stale generated
  artifacts, and rerun/idempotency of generators;
- private data, secrets, hostnames, IPs, credentials, internal URLs, employer
  identifiers, and AI attribution;
- docs, package docs, root `AGENTS.md`/`CLAUDE.md` lockstep, `.codex/skills`
  and `.claude/skills` discovery, hooks, pre-commit, pre-push, and GHA parity;
- follow-on validation needs when the PR cannot honestly prove a separate runtime,
  backend-version, cassette, full-corpus, or performance condition.

## Pass 4: Hostile Read And Abuse Cases

Read the diff as a future rushed agent, tired merger, or bot reviewer trying to
satisfy the letter while violating Eshu's intent. This pass is mandatory even
for docs-only and skill-only changes.

Ask and answer:

- What claim could this PR make too early?
- What proof could be deferred even though it is in scope?
- What wording allows a silent fallback, broad skip, or "follow-up" escape?
- What test could pass while the production subject is still broken?
- What generated artifact, cassette, snapshot, or registry could drift without
  this review catching it?
- What rebase, force-push, or stale-review sequence could make the reviewed diff
  differ from the pushed/merged diff?
- What would an operator be unable to diagnose at 3 AM from telemetry alone?
- What would NornicDB do if one label, index, constraint, or query shape differs
  from the happy path?

Classify every hostile-read finding with one class:

| Class | Meaning |
| --- | --- |
| `wording-loophole` | Text permits behavior the author says they did not intend. |
| `scope-smuggling` | In-scope work is being treated as a follow-up or unrelated risk. |
| `evidence-overclaim` | The PR claims proof that the attached evidence does not provide. |
| `false-green-proof` | A test/gate can pass without exercising the production failure mode. |
| `stale-diff-risk` | Rebase, force-push, generated output, or unresolved review state can invalidate the review. |
| `runtime-proof-gap` | Required backend, scaled, full-corpus, or operator proof is missing. |
| `generated-drift-risk` | Generated artifacts, registries, cassettes, snapshots, or docs can drift from source truth. |

## Eshu Failure Classes To Name Explicitly

Every review must state whether the diff could trigger any of these classes and
where the proof lives:

- false-green tests;
- golden-corpus or B-12 snapshot drift;
- stale generated artifacts or stale discovery registries;
- NornicDB planner fallback or version-skewed optimizer assumptions;
- route, API, MCP, CLI, or OpenAPI mismatch;
- materialization, graph projection, or query-surface disagreement;
- concurrency, lease, retry, idempotency, or ordering bugs;
- telemetry coverage gaps or missing operator-facing evidence;
- private-data, secret, or AI-attribution leakage.

## Finding Schema, Severity, And Disposition

Every finding must include:

- pass: `0`, `1`, `2`, `3`, or `4`;
- class: one hostile-read class or `correctness`, `performance`,
  `concurrency`, `security`, `docs`, `workflow`;
- severity: `P0`, `P1`, or `P2`;
- confidence: `high`, `medium`, or `low`;
- disposition: one of the allowed dispositions below;
- file:line or exact evidence location;
- violated Eshu rule, skill, contract, or proof tier;
- concrete fix and verification that would close it.

Severity:

- **P0**: correctness, data loss, security/private-data leak, main break, or
  deadlock. Blocks commit, push, PR, and merge-readiness.
- **P1**: accuracy regression, missing idempotency/retry/ordering, silent
  failure, false-green test, missing runtime telemetry, unmeasured performance
  change on a hot path, or required proof tier not run. Blocks push/PR update
  until fixed and re-reviewed.
- **P2**: edge case, doc drift, genuine missing coverage, minor performance or
  naming issue. Fix inline or link/create a tracked repository issue. Informal
  TODOs, comments, or memory notes are not enough.

Disposition must be one of: `fixed`, `not-a-bug-with-evidence`,
`deferred-to-linked-follow-up`, or `blocked`. No finding may disappear between
review passes.

## Hard Blocks

The verdict is `blocked` when any of these are true:

- base/head are not the final rebased diff to be pushed or merged;
- proof tier is missing, wrong, or not actually run for in-scope behavior;
- P0 or P1 finding remains unresolved;
- P2 finding is neither fixed nor linked to an explicit tracked repository
  issue;
- generated artifacts or cassettes changed without source-of-truth proof;
- root `AGENTS.md` and `CLAUDE.md` drift;
- public text contains private data, credentials, internal identifiers, or AI
  attribution;
- review comments exist on the latest head and are unresolved;
- CI/check evidence does not match the changed surface.

## Output Template

```text
Eshu code review verdict: self-review|separate-review
Base/head: <base>..<head>
Proof tier decision: <one tier>
Why sufficient: <rationale; name cassette/golden assertions or missing runtime condition>

Pass 0 - Scope, ownership, and diff integrity:
- Findings: <pass, class, severity, confidence, disposition, file:line, rule/skill, fix>

Pass 1 - Correctness and truth:
- Findings: <pass, class, severity, confidence, disposition, file:line, rule/skill, fix>

Pass 2 - Performance and storage/query shape:
- NornicDB pinned/current source checked: <image/tag/digest and source commit/docs>
- Expected fast paths/fallbacks: <flags and evidence>
- Findings: <pass, class, severity, confidence, disposition, file:line, rule/skill, fix>

Pass 3 - Reliability, concurrency, security, workflow hygiene:
- Findings: <pass, class, severity, confidence, disposition, file:line, rule/skill, fix>

Pass 4 - Hostile read and abuse cases:
- Findings: <pass, class, severity, confidence, disposition, file:line, rule/skill, fix>

Cross-pass comparison:
- Contradictions/repeated findings: ...
- Eshu failure classes checked: ...
- Hostile-read classes checked: ...
- Generated artifacts checked: ...
- Private-data/AI-attribution scan: ...
- Follow-on validation required: ...

Disposition:
- P0=<n>, P1=<n>, P2=<n>
- Fixed/reviewed again: <evidence>
- Deferred follow-ups: <links and reasons>

Verification evidence inspected:
- <commands and runtime proof>
```

Ready means `P0=0`, `P1=0`, every P2 fixed or linked to a tracked repository
issue, the selected proof tier is actually run for all in-scope behavior,
out-of-scope proof gaps are routed to tracked follow-ups without overstating
readiness, and the review was repeated after fixes.

## Sample Follow-On Routing

For a default NornicDB image upgrade:

```text
Proof tier decision: Full remote corpus required.
Why sufficient: cassette/golden replay can prove functional graph/API/MCP truth
on the new image, but not startup/schema lock waits, optimizer hot-path
eligibility, image digest drift, graph-write budgets, queue-zero behavior, or
NornicDB CPU/heap attribution.
Follow-on validation required: route backend image pin, digest, startup,
hot-path, performance, and full-corpus validation to the owning follow-up. This
PR must not claim those conditions are proven unless that evidence is attached.
```
