# Eshu Development Guide For AI Agents

Eshu is a code-to-cloud context graph for CLI, MCP, and
HTTP API workflows. Treat it as a production data platform, not a script
collection.

The current branch is a Go-owned runtime:

- **API** serves HTTP reads and admin/query surfaces.
- **MCP Server** serves tool-facing read workflows.
- **Ingester** owns repo sync, discovery, parsing, and fact emission.
- **Reducer / Resolution Engine** owns queued projection, repair, and shared
  materialization.
- **Bootstrap Index** owns one-shot local or deployment seeding.

There is no Python runtime on the normal platform path. Python remains only in
fixture corpora used to validate parser behavior.

## Mandatory Repo Rules

- MUST use `rg` for all text/content searches. NEVER use `grep`.
- MUST use `rg --files` or globbing for file discovery. NEVER use `find`.
- MUST use the Grep tool, backed by `rg`, instead of shell `grep`.
- MUST read local repo docs before searching code or the web.
- MUST ask when intent, architecture, risk, or active ADR ownership is unclear.
- MUST apply TDD when writing or modifying code.
- MUST keep files under 500 lines; split modules before they approach the limit.
- MUST NEVER add AI attribution to commits, PRs, or docs.
- MUST NEVER push to `main` or `master`.
- MUST ALWAYS create git worktrees before executing plans or PRDs.
- MUST use the same branch/worktree name across repositories when one workflow
  touches multiple repos, so related changes can be audited together.
- MUST follow the Google Python Style Guide for any Python fixtures or tools.
- MUST follow Effective Go and the official Go style guide for all Go code.
- MUST use strict mode and proper typing for TypeScript; no `any` without an
  explicit justification.
- MUST follow HashiCorp best practices for Terraform.
- MUST follow Helm chart best practices, including helpers, `NOTES.txt`, and
  values schema.

## Priority Order

Every technical decision follows this order:

1. **Accuracy** - wrong graph truth, query truth, or deployment truth is a
   product failure.
2. **Performance** - prove the correct path can scale to repo-scale inputs.
3. **Reliability** - preserve correctness and measured performance while making
   the system recoverable and operable.

Do not optimize a behavior you have not proven correct. Do not make a system
more reliable by hiding wrong results, swallowing failures, or inventing silent
fallbacks.

Performance is a product contract, not a cleanup task. Contributors MUST NOT
introduce unmeasured performance regressions. New features MUST be designed for
repo-scale inputs from the start, with bounded work, observable stages, and a
clear path to faster execution after correctness is proven.

## Read These First

Before changing runtime, deployment, ingestion, parsing, graph, queue, or
observability behavior, read these pages in order:

1. `docs/docs/deployment/service-runtimes.md`
2. `docs/docs/reference/local-testing.md`
3. `docs/docs/reference/telemetry/index.md`
4. `docs/docs/architecture.md`

If a change affects Docker Compose, also read:

- `docs/docs/deployment/docker-compose.md`

If a change touches Cypher in a hot path (canonical writer, reducer
projection, query handler, materialization job, schema DDL), also read:

- `docs/docs/reference/cypher-performance.md`

That page mandates two pre-implementation checks: research the pinned
backend's actual behavior (NornicDB-New source, Neo4j docs+changelog) and
capture a before/after benchmark on the same inputs against the pinned
binary. Skipping either step is research debt, not a finished change.

If a change affects NornicDB knobs or compatibility, also read:

- `docs/docs/reference/nornicdb-tuning.md`
- `docs/docs/reference/nornicdb-pitfalls.md`
- `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`
- `docs/docs/adrs/2026-04-20-embedded-local-backends-implementation-plan.md`

When debugging NornicDB-shaped failures (false constraint violations,
MERGE not finding existing nodes, re-projection conflicts), read
`docs/docs/reference/nornicdb-pitfalls.md` BEFORE writing or proposing a
NornicDB patch. That page captures known behaviors that have caught Eshu
off guard, and the validation guidance there short-circuits speculative
patching. Always validate the documented behavior against the
NornicDB-New fork source (`/Users/asanabria/os-repos/NornicDB-New`) and
the upstream changelog for the pinned binary tag before relying on the
pitfall write-up — NornicDB evolves quickly and a documented behavior may
already be fixed upstream.

## Communication Style

Talk to the user like a peer, not a lecturer. Plain language first; jargon only
when it earns its keep. The repo owner has been clear: terminology without
context throws them off, and a "speak to me like a bro" register lands better
than corporate or textbook prose.

- **Default register**: conversational, direct, second-person ("you"), short
  sentences, no padding ("Hey here's the deal:" beats "I'd like to provide
  an update on the status of the operation").
- **Jargon rule**: if a domain term (NornicDB phase-group executor,
  `executeEntityPhaseGroupStreaming`, Bolt session, MERGE constraint) shows
  up in an explanation, follow it with a plain-English gloss the first time
  it appears in a thread. Code identifiers stay verbatim; the gloss explains
  what they DO in human terms.
- **Status updates**: lead with the punchline, then the numbers, then the
  caveats. Do not bury the answer under setup.
- **When a step is technical but unavoidable**: name it ("this is the
  database write step"), then say what it means for the run ("this is where
  the slow stage from last time was").
- **Acronyms and three-letter symbols** (CPU, MERGE, MCP, ADR, CI): use
  them, but only after the first use is anchored to a plain noun ("the MCP
  tool — the local API the AI assistants call into").
- **Hedging budget**: one hedge per claim, not three. "About 110 s, give or
  take" beats "approximately 110 seconds, though this number may vary".
- **Bullet vs prose**: use bullets for parallel items or shopping lists, use
  prose for explanations of cause and effect. Switching between them with
  no reason is a tell of AI writing.

This register applies in chat, status updates, PR descriptions, commit
messages explanations to the user, and any narrative the assistant writes
back to the operator. It does **not** apply inside code comments, doc
prose, ADRs, or commit message bodies — those follow the existing
project tone (precise, evidence-heavy, audience = future maintainer).
The user only needs the plain register when they are reading the
assistant's narrative to them in chat.

## Skill Routing

For Eshu runtime diagnostics, reducer throughput, graph backend performance,
queue behavior, local/CI proof runs, and ADR evidence updates, start with the
`eshu-diagnostic-rigor` skill.

Add specialized skills only when the change touches that surface:

- `golang-engineering` for Go code edits and Go tests.
- `cypher-query-rigor` for graph query/write/index or backend dialect work.
- `concurrency-deadlock-rigor` for workers, leases, conflict keys, retries, or
  queue ordering.
- `eshu-correlation-truth` for correlation, materialization truth, or query
  truth.
- `eshu-mcp-call-rigor` for MCP/API tool calls, new MCP tool contracts, local
  MCP diagnostics, or any graph-backed query that could become unbounded.
- `eshu-release` for open-source release, versioning, image, Helm chart, and
  GitHub Release work.
- `skill-creator` for creating or updating skills.

## Golden Rules

### 1. Understand The Flow Before Touching Code

Do not modify code until you can explain the relevant path end to end:

```text
sync -> discover -> parse -> emit facts -> enqueue work -> reducer -> graph/content projection -> query surface
```

For non-trivial changes, map:

- entry point or orchestrator
- where data enters
- how it is transformed
- where it is persisted
- who consumes it
- what runs before and after it
- required data dependencies
- re-trigger or requeue path for downstream consumers
- transaction boundaries
- async boundaries and retries
- ownership boundaries
- invariants assumed by each step

Before editing a handler, reducer domain, collector stage, storage adapter, or
runtime worker, read the owning entrypoint or orchestrator first (`cmd/*`,
bootstrap flow, reducer/workflow runner, or collector snapshot path). Editing a
leaf without understanding who calls it and who consumes its output is not
acceptable.

If the flow is unclear, research first. If intent or active architecture is
still unclear after local research, ask.

### 2. Prove Value Before Calling Work Ready

Every change needs evidence appropriate to its risk:

- bug fixes need a failing regression test first
- performance work needs before/after benchmarks or runtime measurements
- runtime or ingestion changes need small, medium, and large repository timing
  evidence when they can affect end-to-end indexing cost
- queue/concurrency work needs contention, retry, idempotency, and ordering
  proof
- graph truth work needs fixture intent, graph truth, and API/query truth
  agreement
- runtime changes need telemetry and operator diagnosis paths
- doc-only changes need the docs build gate when they affect docs navigation or
  project guidance

Do not say work is ready without listing the commands or runtime proof actually
run.

### 3. Root Cause Beats Patches

Do not paper over symptoms with shallow workarounds, silent fallbacks, or
speculative "good enough" fixes.

Required debugging shape:

1. Gather evidence.
2. Form hypotheses.
3. Prove or disprove each likely cause.
4. Fix the actual failure mode.
5. Add regression coverage and telemetry when runtime behavior changed.

Small diffs are welcome only when they fix the right design.

### 4. Research External Contracts Before Editing

When work touches an SDK, framework, API, database, queue, distributed system,
transaction model, or concurrency guarantee, read the official documentation
before proposing or implementing the change. Local repo docs still come first,
but intuition about external contracts is not enough.

### 5. Edge Cases Are Mandatory

Before implementing any bug fix or design change, account for:

- invalid input
- empty state
- stale state
- partial failure
- duplicate delivery
- retries
- ordering issues
- idempotency
- concurrency
- rollback behavior

For correlation or materialization changes, include one positive case, one
negative case, and one ambiguous case before claiming the design is understood.

### 6. Preserve Service Boundaries

Do not collapse ownership boundaries casually.

| Area | Owns |
| --- | --- |
| `go/internal/collector/` | Git collection, discovery, snapshotting, parsing inputs |
| `go/internal/parser/` | parser registry, adapters, language behavior, SCIP support |
| `go/internal/facts/` | durable fact models and queue contracts |
| `go/internal/storage/postgres/` | facts, queue, status, content, recovery, decisions |
| `go/internal/storage/cypher/` | backend-neutral Cypher write contracts, canonical graph writers, edge helpers, and write instrumentation |
| `go/internal/storage/neo4j/` | Neo4j-specific graph adapters |
| `go/internal/projector/` | source-local projection stages |
| `go/internal/reducer/` | cross-domain materialization and shared projection |
| `go/internal/relationships/` | Terraform, Helm, Kustomize, Argo extraction |
| `go/internal/query/` | HTTP handlers, OpenAPI, query/read surfaces |
| `go/internal/runtime/` | admin, status, probes, retry policy, lifecycle |
| `go/internal/status/` | pipeline and request lifecycle reporting |
| `go/internal/telemetry/` | OTEL tracing, metrics, structured logs |
| `go/internal/truth/` | canonical truth contracts |

Handlers depend on ports such as `GraphQuery` and `GraphWrite`, not concrete
backend implementations. Backend dialect differences belong only in documented
narrow seams.

### 7. Observability Is Part Of The Feature

Every runtime-affecting code change must include telemetry operators can use at
3 AM.

Ask:

- Is it stuck?
- Is it slow?
- Is it failing?
- Is it using too much memory?
- Did it finish?

If metrics, traces, logs, and status surfaces cannot answer those questions,
the design is incomplete.

### 8. Compatibility Without Hidden Branches

Eshu supports `ESHU_GRAPH_BACKEND={neo4j,nornicdb}` behind graph ports.

- `nornicdb` is the officially supported default backend used in Compose and
  production.
- `neo4j` is an alternative backend only when it can run Eshu's shared
  Cypher/Bolt contract without a separate writer stream.

Invalid backend values must fail at startup. Backend selection must surface in
telemetry as `graph_backend` and optionally in response truth metadata as
`truth.backend`.

Do not add backend branches outside documented narrow seams such as schema DDL,
connection/runtime settings, retry classification, and query builders. A new
Cypher/Bolt backend must support the raw Eshu Cypher calls or require only minor,
evidence-backed adapter differences.

Schema-first validation is mandatory for graph-backend performance evidence.
Manual production-profile runs must apply `eshu-bootstrap-data-plane` before
`eshu-bootstrap-index`; otherwise missing graph indexes or constraints can make
the shared Cypher path look falsely slow. Treat any backend comparison that
skips the data-plane schema step as non-acceptance evidence.

## Runtime Contract

| Runtime | Responsibility | Command | Kubernetes shape |
| --- | --- | --- | --- |
| API | HTTP API, admin/query reads | `eshu api start --host 0.0.0.0 --port 8080` | `Deployment` |
| MCP Server | MCP tool server | `eshu mcp start` | `Deployment` or sidecar |
| Ingester | Repo sync, parse, fact emission | `/usr/local/bin/eshu-ingester` | `StatefulSet` + PVC |
| Reducer | Queue drain, graph projection, repair flows | `/usr/local/bin/eshu-reducer` | `Deployment` |
| Bootstrap Index | One-shot initial indexing | `/usr/local/bin/eshu-bootstrap-index` | job / init step |

Shared backing stores:

- **NornicDB** for the canonical graph by default; Neo4j only for explicit
  compatibility deployments that meet the shared Cypher contract
- **Postgres** for facts, queue state, content store, status, and recovery data

## Local Development

Full stack:

```bash
docker compose up --build
```

## Runtime Repro Hygiene

Before any dogfood, local-authoritative, Compose, or runtime validation that
executes local Eshu binaries, rebuild them first:

```bash
cd go
go build -o ./bin/eshu ./cmd/eshu
go build -o ./bin/eshu-api ./cmd/api
go build -o ./bin/eshu-ingester ./cmd/ingester
go build -o ./bin/eshu-reducer ./cmd/reducer
export PATH="$PWD/bin:$PATH"
```

`eshu graph start` discovers `eshu-reducer` and `eshu-ingester` through `PATH`, so
fresh owner runs need `go/bin` on `PATH`.

When building or testing NornicDB binaries from the local reference repos, use
the no-local-LLM tags first:

```bash
go test -tags 'noui nolocalllm' ./...
go build -tags 'noui nolocalllm' ...
```

### NornicDB Maintainer Patch Bar

Eshu maintainers are allowed to patch NornicDB, but only when the change is
evidence-backed:

- a correctness fix for NornicDB itself,
- a measured NornicDB performance win that generalizes beyond one Eshu symptom,
  or
- a measured Eshu runtime win proven by focused and corpus-level evidence.

Do not keep NornicDB patches for speculative Eshu throughput hypotheses. If a
patch does not produce a real backend or Eshu win, revert it and continue testing
against upstream `main` or the latest owner-merged build.

## TDD And Bug Fix Workflow

For bugs, use this mandatory sequence:

1. Write a failing test that reproduces the exact bug condition.
2. Run the focused test and verify it fails for the expected reason.
3. Implement the smallest correct fix at the right ownership boundary.
4. Re-run the focused test and verify it passes.
5. Add regression variations for edge cases, retries, ordering, or concurrency
   when relevant.
6. Run the smallest package or integration gate that proves the touched
   contract.

For new features, write the contract or behavioral test first unless the work
is pure documentation.

## Performance Workflow

Performance work must show measurable value:

1. Capture a baseline with a benchmark, trace, metric sample, runtime status
   report, or focused compose proof.
2. Identify whether the bottleneck is algorithmic, allocation-heavy,
   concurrency-related, graph I/O, Postgres I/O, parser behavior, or input
   shape.
3. Change the narrowest layer that owns the bottleneck.
4. Capture after-data with the same measurement.
5. Document material trade-offs, including memory, queue depth, and failure
   behavior.

Any runtime, parser, collector, reducer, graph, storage, or query change that
can affect indexing throughput MUST preserve or improve measured end-to-end
time for comparable inputs. If a brand-new capability costs more, document the
cost, why it is necessary for correctness, and how the design keeps the work
bounded as repositories grow.

Keep a size-aware performance trail for dogfooding and regression review:

- small repo: fast sanity proof and correctness checks
- medium repo: realistic mixed-language or mixed-deployment proof
- large repo: stress proof for graph writes, queue behavior, parser cost, and
  query surfaces

Record repository size signals, wall time, terminal queue state, indexed file
count, fact count, stage timings, backend, and commit id whenever a run is used
as performance evidence.

Do not lower graph-write timeouts, global batch sizes, or worker counts because
one repository is noisy. First use `eshu index --discovery-report` and consider a
repo-local `.eshu/discovery.json` or process-local discovery overlay.

## MCP And API Call Performance

MCP, HTTP API, and direct graph helper calls must be bounded before they run.
Correctness still comes first, but an accurate call contract must also be
scoped, cancellable, observable, and cheap to fail.

Before designing or running a potentially expensive read:

- resolve the canonical scope first (`repo_id`, `workload_id`, `service_id`, or
  `environment`); do not default to the whole graph when a local scope is
  available
- require a `limit`, timeout, deterministic ordering, and `truncated` signal for
  list-style tools
- run a cheap preflight for local MCP: owner record, current Postgres port,
  current graph/Bolt port, repo freshness, and profile/truth label
- prefer two-step calls: summary/count/handles first, payload or drilldown
  second
- keep high-volume or per-node metadata out of graph hot paths unless a measured
  indexed query needs it
- if a call is slow, stop and classify the delay as transport, stale local owner
  ports, backend health, query shape, payload size, or missing index before
  retrying

Runtime modes with different performance profiles must require explicit opt-in;
an auxiliary env var must not silently move a normal local run onto a slower
runtime.

## Concurrency Workflow

Before changing workers, leases, retries, queues, transactions, or shared graph
writes, describe:

- shared state
- lock or claim ordering
- transaction scope and duration
- retry boundaries
- idempotency keys
- conflict domains
- starvation and contention risks
- write amplification
- dead-letter behavior

Research the actual locking and consistency behavior of Postgres, Neo4j,
NornicDB, or the Go runtime path in use. Never rely on intuition alone.

## Facts-First Bootstrap Ordering

The bootstrap-index orchestrator in `go/cmd/bootstrap-index/main.go` runs a
multi-pass pipeline. Editing reducer or projector domains without understanding
this ordering creates E2E-only bugs.

```text
Phase 1 - Collection + First-Pass Reduction
  bootstrap-index collects repos and emits facts.
  resolution-engine drains first-pass domains.
  deployment_mapping can remain pending because resolved_relationships do not
  exist yet.

Phase 2 - Backfill
  BackfillAllRelationshipEvidence() populates relationship_evidence_facts and
  publishes readiness rows.

Phase 3 - Deployment Mapping Reopen
  ReopenDeploymentMappingWorkItems() reopens deployment_mapping so the reducer
  can create resolved_relationships.

Phase 4 - Second-Pass Consumers
  Any domain that consumes resolved_relationships must have a re-trigger
  mechanism after Phase 3.
```

Key rule: any domain that consumes `resolved_relationships` must have a
post-Phase-3 reopen or re-trigger mechanism.

## Correlation Truth Gates

Use `eshu-correlation-truth` whenever a change touches workload admission,
deployable-unit correlation, materialization, deployment tracing, or query truth
in `go/internal/reducer`, `go/internal/query`, `go/internal/graph`,
`go/internal/relationships`, or correlation fixtures.

Required proof:

- explain raw evidence -> candidate -> admission -> projection row -> graph
  write -> query surface
- include positive, negative, and ambiguous cases
- prove what materializes and what remains provenance-only
- validate utility repos, controller repos, deployment repos, and ambiguous
  multi-unit repos
- run a fresh rebuild/restart path before blaming timing
- compare fixture intent, reducer graph truth, and API/query truth

Namespace, folder, or repo-name heuristics must not invent environment or
platform truth unless backed by explicit environment aliases or stronger
deployment evidence.

## Observability Contract

Every runtime-affecting code change must include telemetry.

| Change type | Required telemetry |
| --- | --- |
| New pipeline stage or worker | OTEL span, duration histogram, success/failure counter |
| New Postgres or Neo4j query | Duration histogram via `InstrumentedDB`, error counter |
| New queue consumer | Claim duration histogram, processing duration histogram, depth gauge |
| New retry/skip path | Counter with reason label, structured log with `failure_class` |
| Memory or resource tuning | Observable gauge reporting configured limit |
| Batch processing | Batch size histogram, batches committed counter |

Implementation rules:

- Metrics live in `go/internal/telemetry/instruments.go`.
- Metric names use the `eshu_dp_` prefix.
- New metric dimensions go in `go/internal/telemetry/contract.go`.
- Spans use `tracer.Start(ctx, telemetry.SpanXxx)`.
- New span names go in `contract.go`.
- Structured logs use `slog` with `telemetry.ScopeAttrs()`,
  `telemetry.PhaseAttr()`, and `telemetry.FailureClassAttr()`.
- Log keys are frozen in `contract.go`; reuse existing keys before adding new
  ones.
- High-cardinality values such as file paths and fact IDs belong in spans or
  logs, not metric labels.

## Cypher Work

Any time you read, write, debug, or optimize Cypher — NornicDB or Neo4j —
research the docs first. **Accuracy, performance, and concurrency** is the
motto; you cannot promise any of them without knowing what the engine actually
does with your query.

Required reading before any non-trivial Cypher change:

- `~/os-repos/NornicDB-New/docs/performance/hot-path-query-cookbook.md` —
  identify which recognized hot-path shape your query matches, or note that
  none applies.
- `~/os-repos/NornicDB-New/nornicdb-failing-query-shapes.md` — confirm your
  shape is not on the known-bad list.
- The relevant `pkg/cypher/*hotpath*_test.go` files in NornicDB-New — those
  tests are NornicDB's empirical contract for what shape engages which fast
  path.

Before proposing a change:

- Trace the exact dispatch path the production query takes — which executor
  handler, which fast path or fallback — using profile or log evidence, not
  assumption.
- State which fast path your fix will engage and why. If you cannot make that
  statement with evidence, stop and ask.

The `cypher-query-rigor` skill encodes the rest of the discipline.

## NornicDB Compatibility Workflow

When Eshu hits a NornicDB incompatibility such as Cypher parse rejection,
rollback behavior, driver shape mismatch, or a missing procedure:

1. Check upstream source before guessing:
   - `/Users/allen/os-repos/NornicDB/`
   - `/Users/allen/os-repos/NornicDB-eshu-bolt-rollback/`
2. Decide from evidence:
   - if NornicDB supports it, fix Eshu
   - if NornicDB has a workaround, use a documented backend-dialect seam
   - if NornicDB must be patched, land the fix in the ESHU-maintained fork,
     rebuild, and pin the binary until upstream absorbs it
3. Record the decision in the NornicDB ADR adapter evidence row and the active
   embedded-local-backends chunk status row.

When adding or changing any `ESHU_NORNICDB_*` tuning knob, update the tuning
reference, active ADR, and local testing runbook in the same PR.

## Documentation Discipline

Every code PR that touches user-visible wire contracts, CLI flags, environment
variables, runtime profiles, capability ports, collector plugin contracts, or
chunk boundaries must include:

1. Update the active ADR `## Chunk Status` table or equivalent tracker. If you
   are unsure which ADR is active, ask.
2. Update affected user-facing docs:
   - `docs/docs/reference/http-api.md`
   - `docs/docs/reference/cli-reference.md`
   - `docs/docs/guides/mcp-guide.md`
   - `docs/docs/why-eshu.md`
   - `docs/docs/architecture.md`
   - `docs/docs/getting-started/*`
3. Add `doc.go` for any new Go package, with a package-level comment naming
   the spec it implements.
4. Document every new or touched exported Go type, interface, function, method,
   constant group, and variable with a useful Go doc comment. The comment must
   explain the contract, invariant, failure mode, or operational reason for the
   API; placeholder comments that only repeat the identifier are not acceptable.
5. Add comments for new or touched unexported Go helpers when they encode a
   contract, storage/query assumption, concurrency rule, retry rule, or
   regression purpose.
6. Every Go package directory in `go/` has three files: `doc.go`,
   `README.md`, and `AGENTS.md`. `doc.go` carries the package contract for
   `go doc` consumers (real exported identifiers, invariants, failure
   modes). `README.md` carries the architectural and operational lens for
   human contributors — pipeline-position mermaid, internal-flow mermaid,
   lifecycle prose, exported surface, dependencies, telemetry the package
   emits, operational runbook notes, extension points, and gotchas with
   `file.go:line` cites. `AGENTS.md` carries guidance for LLM assistants
   editing the package — read-first ordered file list, invariants citing
   file:line, common changes scoped by file, failure modes mapped to
   metric/log/span, package-specific anti-patterns, and what NOT to change
   without an ADR. Three audiences, three files, no duplication. The
   project skills live in `.agents/skills/` and are symlinked into
   `.claude/skills/` and `.codex/skills/` so Claude Code and Codex use the same
   repository-owned instructions. The `eshu-folder-doc-keeper` skill defines the
   writing standards.
   The PostToolUse hooks at `.claude/hooks/eshu-doc-staleness.sh` (Claude
   Code) and `.codex/hooks/eshu-doc-staleness.sh` (Codex) flag drift in
   `.eshu-doc-state/stale.jsonl`. The slop gate at
   `scripts/verify-doc-claims.sh` confirms every backticked Go identifier
   in `README.md` and `AGENTS.md` appears literally in source, every
   `file.go:NN` cite resolves to a real line, and no marketing words
   leaked through. Run the verifier on a package before committing
   doc-only changes there. Container directories without Go source
   (`go/`, `go/cmd/`, `go/internal/`, `go/internal/storage/`,
   `go/internal/terraformschema/schemas/`) keep `README.md` only — they
   are not Go packages. Docs directories may use `index.md`; generated,
   vendor, build, cache, and fixture leaf directories are exempt.
7. Keep OpenAPI changes in lockstep with `go/internal/query/openapi*.go`,
   handler tests, and `docs/docs/reference/http-api.md`. Do not document
   Swagger UI or ReDoc routes unless the server actually registers them.
8. Document new extensibility seams in `docs/docs/architecture.md`,
   `docs/docs/why-eshu.md`, and a dedicated reference page.

`AGENTS.md` mirrors `CLAUDE.md`. Any edit to one must be mirrored in the other
in the same PR.

## Verification Defaults

Use `docs/docs/reference/local-testing.md` as the source of truth.

Common gates:

```bash
cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./internal/query ./internal/mcp -count=1
cd go && go test ./internal/parser ./internal/collector/discovery ./internal/content/shape ./internal/collector -count=1
cd go && go test ./internal/terraformschema ./internal/relationships -count=1
cd go && go test ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer ./internal/runtime ./internal/status ./internal/storage/postgres -count=1
cd go && golangci-lint run ./...
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Docs, `CLAUDE.md`, `AGENTS.md`, or README changes require the docs build plus
`git diff --check`:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Doc-keeper Workflow

Every Go package directory in `go/` carries three files: `doc.go`,
`README.md`, and `AGENTS.md`. Project skills live in `.agents/skills/` and are
symlinked into `.claude/skills/` and `.codex/skills/`; `eshu-folder-doc-keeper`
defines the writing standards. A PostToolUse hook for Claude Code
(`.claude/hooks/eshu-doc-staleness.sh`, matcher `Edit|MultiEdit|Write`)
and one for Codex (`.codex/hooks/eshu-doc-staleness.sh`, matcher
`^apply_patch$`) both delegate to a tool-neutral
`scripts/check-docs-stale.sh`, which writes a JSONL drift snapshot to
`.eshu-doc-state/stale.jsonl` (gitignored). A separate slop gate at
`scripts/verify-doc-claims.sh` validates that documentation claims are
grounded in source.

Workflow:

1. After editing files under `go/`, run the drift check if you are using a
   tool that does not have hooks installed:

   ```bash
   scripts/check-docs-stale.sh --all
   ```

   The script is `stat`-based and fast. `--all` rebuilds the snapshot from
   scratch every run.

2. If `.eshu-doc-state/stale.jsonl` is non-empty, invoke the
   `eshu-folder-doc-keeper` skill before committing. The skill reads the
   snapshot, scopes its update to the directories it names, and refreshes
   only the affected sections of `README.md`, `AGENTS.md`, and `doc.go`.

3. Before committing doc-only changes to a package, run the slop gate on
   that package:

   ```bash
   scripts/verify-doc-claims.sh go/internal/<pkg>
   ```

   The verifier (a) confirms every backticked Go identifier in `README.md`
   and `AGENTS.md` appears literally in the package's `.go` files (or is
   in the explicit allowlist of stdlib / project-wide names), (b) checks
   every `file.go:NN` cite — file exists in the package, line is within
   EOF, and at least one identifier from the same paragraph appears within
   ±10 lines of the cited line — to catch citation drift, (c) runs an
   anti-marketing pass that fails on `leverages, seamlessly, robust,
   powerful, comprehensive, key role, stands as, serves as, underscores,
   showcases, facilitates, delves`. Run with `--all` to walk every Go
   package under `go/`.

4. The drift check hook is a snapshot, not a log: it overwrites
   `stale.jsonl` on each `--all` run, so you do not need to clear it
   manually. If you want to keep history, rotate the file to a `.resolved`
   sibling before the next tool use.

5. Both scripts are suitable as git `pre-commit` hooks; install them
   locally with the CI/CD pre-commit framework or a thin wrapper.

Container directories without Go source (`go/`, `go/cmd/`, `go/internal/`,
`go/internal/storage/`, `go/internal/terraformschema/schemas/`) keep
`README.md` only — they are not Go packages, so `doc.go` would not compile
and the LLM-assistant `AGENTS.md` is not useful where there is no code to
edit.

## Remote Build Hygiene

When rebuilding Go projects over non-interactive SSH:

- do not assume the remote shell loads the same `PATH`
- check `command -v go` and common absolute paths such as `/usr/local/go/bin/go`
- if Go exists only at an absolute path, use that path for remote `go build`
  and `go test`
- keep hostnames, IPs, private key paths, and machine-specific repo paths out of
  open-source docs

## Pre-Ready Checklist

Before saying work is complete:

- repo docs read for the touched surface
- relevant skill used
- data/control flow understood end to end
- tests written first for code changes
- edge cases considered
- telemetry added for runtime behavior
- docs and active ADRs updated for contract changes
- `AGENTS.md` and `CLAUDE.md` kept in lockstep if either changed
- `golangci-lint run ./...` clean for Go changes
- focused verification run and cited
- `git diff --check` clean
