# Cypher Performance Discipline

This page is the long-form companion to the `cypher-query-rigor` project
skill (`.agents/skills/cypher-query-rigor/SKILL.md` in the repository root,
symlinked into `.claude/skills/` and `.codex/skills/`). The skill captures
the short mandate; this page captures the workflow, backend-specific
research locations, and the measurement protocol that turns "this Cypher
looks faster" into "this Cypher is measurably faster against the binary we
ship."

Read this page before:

- writing a new Cypher statement that lives in a hot path (canonical writer,
  reducer projection, query handler, materialization job),
- changing the shape of an existing Cypher hot-path statement,
- adding a new index, constraint, or schema element,
- bumping the pinned NornicDB or Neo4j image version.

If you are touching Cypher in a non-hot-path test or one-off script, the
research-first and benchmark-first mandates still apply — they just resolve
faster because the cost ceiling is lower.

## The two mandatory pre-implementation checks

Both must be answered explicitly before merging Cypher into a hot path. If
either answer is "I don't know yet," do not merge yet.

### 1. Research first

Before designing or writing the query, gather what the pinned graph backend
actually supports. Cypher syntax is a written-but-evolving contract; the
backend behind it may not implement every feature identically.

For **Neo4j**:

- Read the [Cypher manual](https://neo4j.com/docs/cypher-manual/current/) for
  the major version pinned in your environment (check `NEO4J_VERSION` /
  `docker-compose.yaml`).
- Read the changelog between the pinned version and the latest release to
  catch planner changes that affect your shape.
- If your query uses a feature added in a recent version (`CALL { }`
  subqueries, list comprehensions, dynamic labels, vector indexes), confirm
  the pinned version supports it before designing around it.

For **NornicDB** (Eshu's default backend):

- The Eshu-maintained fork lives at `/Users/asanabria/os-repos/NornicDB-New`.
  This is the source that the pinned `timothyswt/nornicdb-amd64-cpu:vX.Y.Z`
  binary is built from. Do not read the older sibling at
  `/Users/asanabria/os-repos/NornicDB` — it does not match the running
  binary.
- Read the relevant code under `pkg/cypher/` and `pkg/storage/` in
  NornicDB-New for the exact behavior of the feature you plan to use.
  Particularly: `pkg/cypher/merge.go`, `pkg/cypher/clauses.go`,
  `pkg/storage/badger_transaction.go`, `pkg/storage/schema.go`.
- Read [NornicDB Pitfalls](nornicdb-pitfalls.md) for known traps in the
  current binary.
- Read [NornicDB Tuning](nornicdb-tuning.md) for the runtime knobs that may
  affect your statement.

For **both backends**:

- If your query uses a pattern you have never run against the pinned binary,
  treat it as research debt. Validate the pattern works as documented before
  designing the production statement around it. The cheapest way is a
  focused Go test or a `curl`-against-Bolt-HTTP probe against an isolated
  Compose stack (uniquely named per the
  [Local Testing](local-testing.md) guidance).

Document what you learned in the PR description or commit message: "Verified
against NornicDB-New `<commit>` (`pkg/cypher/merge.go:<line>`); behavior
matches the design assumption that X."

### 2. Benchmark first

Before merging the query, capture before/after evidence appropriate to the
risk. Unmeasured Cypher in a hot path is a regression-shaped surprise waiting
to happen.

The minimum bar is **a baseline measurement and an after measurement against
the same inputs**, both captured against the pinned backend binary.

Acceptable measurement shapes, in order of preference:

1. **Focused Go benchmark.** For statements wrapped by a Go writer
   (`go/internal/storage/cypher/...`), write a `*_bench_test.go` that
   exercises the writer with representative inputs and reports
   `ops/sec`, `ns/op`, and `B/op`. Run `go test -bench=. -benchmem` before
   and after; record the deltas in the PR.
2. **Compose-stage timing.** For statements that only fire end-to-end (e.g.,
   reducer projections, materialization passes), capture the structured-log
   `duration_seconds` for the relevant phase from a small, medium, and large
   fixture corpus. Compose verifier scripts in `scripts/verify_*.sh` are the
   canonical place to add timing assertions if your change can affect a
   measured proof.
3. **Manual reproducer.** For ad-hoc queries (admin runbooks, one-off
   materialization jobs), capture the response time of the pinned-binary
   execution against a representative dataset. Wall time + result row count
   is the floor.

What to record for every measurement:

- **Backend and version** (e.g., `timothyswt/nornicdb-amd64-cpu:v1.0.45`).
- **Storage state**: schema applied via `eshu-bootstrap-data-plane` before
  indexing? (Skipping this step is the most common source of falsely-slow
  baselines on NornicDB; see
  [NornicDB Pitfalls](nornicdb-pitfalls.md).)
- **Input cardinality** at every anchor: how many rows entered, how many
  came out, how many candidate nodes the anchor selected.
- **Index/constraint state**: which indexes existed when the query ran. If
  you added or removed one, capture both states.
- **Plan or statement summary**: Neo4j `PROFILE` if available; NornicDB
  structured-log statement-summary lines if not.

If a Cypher change is purely correctness (e.g., bug fix that does not change
the planned shape), say so explicitly in the PR and explain why a benchmark
is not load-bearing for the decision. Correctness-only changes still need a
"no measurable regression" check against the same input shape, but the
threshold is lower.

## CI evidence gate

Hot-path Cypher and graph-write changes must leave benchmark evidence in the
repo, not only in PR text. CI runs `scripts/verify-performance-evidence.sh`
against the PR diff. The gate is path-based and content-based, so it catches
new collector packages that introduce Cypher strings, graph writes, worker
claims, leases, batching, or concurrency knobs even when the package did not
exist before.

For a hot-path change, update an ADR, reference page, or package README changed
in the same PR with one benchmark marker:

- `Performance Evidence:` for before/after runtime proof.
- `Benchmark Evidence:` for focused `go test -bench` or equivalent microbench
  proof.
- `No-Regression Evidence:` for correctness-only query changes where the same
  input shape showed no measurable regression.

Also include one observability marker:

- `Observability Evidence:` when the change added or used metrics, spans, logs,
  status output, profiles, or queue/domain counters that prove operators can
  diagnose the path.
- `No-Observability-Change:` when existing signals already cover the changed
  path. Name those signals explicitly.

Good evidence note:

```text
Performance Evidence: focused writer benchmark on NornicDB v1.0.45 with
50,000 File rows moved from 820ms to 310ms; full corpus stayed drained at
896/896 repositories with 0 open queue rows.

Observability Evidence: existing eshu_dp_canonical_phase_duration_seconds
and shared-edge summaries expose the phase, row count, and relationship
route; no new metric labels were added.
```

Bad evidence note:

```text
Performance Evidence: looks faster locally.
Observability Evidence: logs are probably enough.
```

## Anti-patterns

These show up in PRs that skipped the pre-implementation discipline. If you
see one in review, ask for the missing measurement.

- "It looks faster" — no baseline captured.
- "I followed the Neo4j docs" — but the binary is NornicDB, and NornicDB does
  not implement the cited feature identically.
- "The unit test passes" — but unit tests don't exercise the hot-path shape
  at production cardinality.
- "The compose run completed" — but the structured logs were not inspected
  for phase timing or constraint violations.
- "Adding an index will help" — index without a baseline is just write
  amplification.

## Backend research locations (quick reference)

| Need | Neo4j | NornicDB |
|------|-------|----------|
| Cypher feature support | [Cypher Manual](https://neo4j.com/docs/cypher-manual/current/) for pinned major | `pkg/cypher/*.go` in NornicDB-New |
| Storage/constraint behavior | [Operations Manual](https://neo4j.com/docs/operations-manual/current/) | `pkg/storage/*.go` in NornicDB-New |
| Known traps | Neo4j changelog | [NornicDB Pitfalls](nornicdb-pitfalls.md) |
| Runtime knobs | Neo4j config reference | [NornicDB Tuning](nornicdb-tuning.md) |
| Version pinning | `NEO4J_VERSION` env | `NORNICDB_IMAGE` env in `docker-compose.yaml` |

## Working with backend-specific behaviors

Eshu prefers backend-neutral Cypher whenever possible. When a backend
diverges on a feature you need, prefer in this order:

1. **Restructure the Cypher** to use a shape that both backends handle
   identically. Often the "advanced" feature has a portable equivalent that
   one or two extra lines of Cypher can express.
2. **Add a narrow dialect seam** in `go/internal/storage/cypher/` (Eshu
   side) with an explicit per-backend code path. Backend-specific code is
   acceptable only in documented seams: schema DDL, connection/runtime
   settings, retry classification, query builders, and measured dialect
   adapters.
3. **Patch the backend** as a last resort. Per the NornicDB Maintainer Patch
   Bar in CLAUDE.md, NornicDB patches require evidence (correctness fix,
   measured perf win, or measured Eshu runtime win). See
   [NornicDB Pitfalls](nornicdb-pitfalls.md) for the workflow.

## Examples from Eshu's history

When a Cypher change in the repo turned out to be measurably correct and
performant, leave a breadcrumb in the PR description that future you can
read. The pattern that has worked:

1. State the hot-path goal: e.g., "module-aware drift joining for
   removed_from_config detection."
2. Quote the baseline measurement: "Phase 3.5 drift admission for bucket F
   takes 220ms against the 2-repo fixture corpus on the gen-1 collector
   instance."
3. State the design's predicted improvement and validate it: "Switching the
   prior-config walk to consult terraform_modules-derived prefixes adds 1
   indexed lookup per row; predicted +5ms, measured +6ms; correctness gain
   covers the case where bucket-F's resource lives inside a module call."

This history then becomes the next person's reference when they're deciding
whether their proposed Cypher change is worth the cost.

## See also

- The `cypher-query-rigor` project skill at
  `.agents/skills/cypher-query-rigor/SKILL.md` in the repository root — the
  short mandate that points here.
- [NornicDB Pitfalls](nornicdb-pitfalls.md) — read this before patching
  NornicDB.
- [NornicDB Tuning](nornicdb-tuning.md) — operator knobs.
- [Local Testing](local-testing.md) — how to run a measured Compose proof.
- [Telemetry Overview](telemetry/index.md) — how Eshu emits the per-phase
  durations and counters you'll read when validating a Cypher change.
