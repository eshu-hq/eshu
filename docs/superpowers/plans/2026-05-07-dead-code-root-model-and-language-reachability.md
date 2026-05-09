# Dead-Code Maturity Implementation Plan

> **For agentic workers:** REQUIRED: Use `superpowers:subagent-driven-development`
> if subagents are available, otherwise use `superpowers:executing-plans`.
> Steps use checkbox syntax for tracking. Language tracks are intentionally
> split so parallel agents can own separate fixture and parser surfaces.

**Goal:** Mature Eshu's `code_quality.dead_code` capability across every
parser-supported source language with dedicated fixtures, language maturity
metadata, root evidence, query classification, and local graph proof.

**Architecture:** Keep the graph-backed candidate scan, but make cleanup safety
language-scoped. Tree-sitter and parser adapters emit syntax/root evidence;
query and reducer code classify candidates with explicit maturity states:
`derived_candidate_only`, `derived`, `ambiguous_only`, and eventually `exact`.

**Tech Stack:** Go, Tree-sitter, SCIP where available, PostgreSQL facts,
NornicDB/Neo4j graph reads, HTTP API, MCP, CLI, OTEL, MkDocs

**Related ADR:**
`docs/docs/adrs/2026-05-07-dead-code-root-model-and-language-reachability.md`

---

## Parallel Execution Rules

- Shared/core query, fact, reducer, MCP, CLI, and telemetry files have one
  owner at a time.
- Language workers may run in parallel only when their write sets are disjoint.
- Each language worker owns `tests/fixtures/deadcode/<language>/`.
- A language worker may touch its parser file only if no other worker owns that
  same file. JavaScript, TypeScript, and TSX share parser files, so use one
  JS-family implementation owner for shared parser changes.
- Every language track starts with fixtures and failing tests before
  implementation.
- Local graph proof happens after integration. The user will restart Eshu graph
  when the branch is ready for API/MCP proof.

## Current Branch Status

- [x] Created ADR and subagent-ready plan.
- [x] Ignored `.planning/` and removed tracked `.planning` files.
- [x] Added dogfood regression tests for Eshu Go false positives.
- [x] Added Go semantic root policy for explicit metadata.
- [x] Projected `dead_code_root_kinds` from graph rows.
- [x] Added initial language maturity metadata.
- [x] Added `tests/fixtures/deadcode/README.md`.
- [x] Added per-result dead-code classification and OpenAPI schema coverage.
- [x] Added Go, Python, JavaScript, TypeScript, and TSX root-model fixtures and
  focused parser/query coverage.
- [x] Added candidate-only fixtures for C, C++, C#, Dart, Elixir, Groovy,
  Haskell, Java, Kotlin, Perl, PHP, Ruby, Rust, Scala, and Swift.
- [x] Split dead-code query helpers so touched files remain under 500 lines.
- [x] Ran local parser/query/CLI, docs, size, and whitespace gates before graph
  restart.
- [x] Re-ran local-authoritative graph proof and found a blocking canonical
  write regression after the dead-code maturity changes.
- [x] Rejected the speculative pre-entity retract experiment after it failed to
  restore the fast path.
- [x] Rejected `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` as the immediate
  fix for this branch after a same-worktree A/B stayed slow.
- [x] Restored local-authoritative self-repo indexing to the previous
  sub-minute envelope by making process mode require
  `ESHU_NORNICDB_RUNTIME=process`.

## Shared Workstream A: Maturity Contract

**Owner:** Core query worker

**Files:**

- Modify: `go/internal/query/code_dead_code_language_maturity.go`
- Modify: `go/internal/query/code_dead_code_language_maturity_test.go`
- Modify: `go/internal/query/code_dead_code.go`
- Modify: `specs/capability-matrix.v1.yaml`
- Modify: `docs/docs/reference/dead-code-reachability-spec.md`
- Modify: `tests/fixtures/deadcode/README.md`

- [x] Add `dead_code_language_maturity` to the `find_dead_code` analysis
  block.
  -> verify: `cd go && go test ./internal/query -run TestHandleDeadCodeReportsLanguageMaturity -count=1`.
- [x] Add a test that every source parser key in `go/internal/parser/registry.go`
  has an explicit dead-code maturity row.
  -> verify: `cd go && go test ./internal/query -run TestDeadCodeLanguageMaturityCoversParserSourceLanguages -count=1` fails first, then passes.
- [x] Keep IaC/config parser keys out of `code_quality.dead_code` maturity and
  point them to IaC usage reachability where applicable.
  -> verify: the maturity coverage test rejects HCL/YAML/JSON/SQL as code
  deadness languages unless explicitly allowed by the test.
- [x] Update docs to define `derived_candidate_only` as parser-supported but
  not cleanup-safe.
  -> verify: docs build passes.

## Shared Workstream B: Query Classification

**Owner:** Core query worker

**Files:**

- Modify: `go/internal/query/code_dead_code.go`
- Modify: `go/internal/query/code_dead_code_go_roots.go`
- Modify: `go/internal/query/code_dead_code_contract_test.go`
- Create: `go/internal/query/code_dead_code_classification_test.go`
- Modify: `go/internal/query/openapi_paths_code.go`

- [x] Add response classification fields for `unused`, `reachable`,
  `excluded`, `ambiguous`, `derived_candidate_only`, and
  `unsupported_language`.
  -> verify: `cd go && go test ./internal/query -run TestHandleDeadCode.*Classification -count=1` fails first, then passes.
- [x] Keep existing `truth.level=derived` until exact language gates are proven.
  -> verify: classification tests assert truth labels separately from result
  classification.
- [x] Update OpenAPI dead-code schema and examples.
  -> verify: `cd go && go test ./internal/query -run 'TestServeOpenAPI|DeadCode' -count=1`.

## Shared Workstream C: Reachability Materialization

**Owner:** Reducer/facts worker

**Files:**

- Modify: `go/internal/facts/models.go`
- Create: `go/internal/facts/dead_code_reachability.go`
- Create: `go/internal/facts/dead_code_reachability_test.go`
- Create: `schema/data-plane/postgres/017_dead_code_reachability.sql`
- Modify: `go/internal/storage/postgres/schema.go`
- Create: `go/internal/storage/postgres/dead_code_reachability.go`
- Create: `go/internal/storage/postgres/dead_code_reachability_test.go`
- Create: `go/internal/reducer/dead_code_reachability.go`
- Create: `go/internal/reducer/dead_code_reachability_test.go`

- [ ] Write failing fact tests for stable IDs keyed by repo, generation,
  language, entity, root category, evidence kind, and source span.
  -> verify: `cd go && go test ./internal/facts -run TestDeadCodeReachability -count=1` fails.
- [ ] Write failing storage tests for idempotent upsert, stale generation
  skips, and paged reads.
  -> verify: `cd go && go test ./internal/storage/postgres -run TestDeadCodeReachability -count=1` fails.
- [ ] Write failing reducer tests for transitive reachability from roots
  through `CALLS`, `IMPORTS`, `REFERENCES`, and semantic root edges.
  -> verify: `cd go && go test ./internal/reducer -run TestDeadCodeReachability -count=1` fails.
- [ ] Implement facts, storage, reducer materialization, stale-generation
  skips, and retry-idempotent writes.
  -> verify: focused facts, storage, and reducer tests pass.

## Shared Workstream D: MCP, CLI, And Telemetry

**Owner:** Surface/observability worker

**Files:**

- Modify: `go/internal/mcp/tools_codebase.go`
- Modify: `go/internal/mcp/tools_test.go`
- Modify: `go/cmd/eshu/analyze.go`
- Modify: `go/cmd/eshu/analyze_test.go`
- Modify: `go/internal/telemetry/contract.go`
- Modify: `go/internal/telemetry/contract_test.go`
- Modify: `go/internal/telemetry/instruments.go`
- Modify: `go/internal/telemetry/instruments_test.go`
- Modify: `docs/docs/reference/mcp-reference.md`
- Modify: `docs/docs/reference/cli-analysis.md`

- [ ] Pass maturity and classification fields through MCP without changing the
  envelope truth labels.
  -> verify: `cd go && go test ./internal/mcp -run DeadCode -count=1`.
- [ ] Pass maturity and classification fields through CLI JSON output.
  -> verify: `cd go && go test ./cmd/eshu -run 'DeadCode|Analyze' -count=1`.
- [ ] Add low-cardinality telemetry for root extraction, candidate
  suppression, ambiguity, classification, and traversal.
  -> verify: `cd go && go test ./internal/telemetry -run DeadCode -count=1`.

## Shared Workstream E: Local-Authoritative Performance Recovery

**Owner:** Runtime/performance worker

**Files:**

- Modify: `go/cmd/eshu/local_authoritative_dead_code_perf_test.go`
- Modify: `go/cmd/eshu/local_authoritative_query_perf_test.go` if shared helper
  extraction is needed
- Modify: `go/cmd/eshu/local_graph.go`
- Modify: `go/cmd/eshu/local_graph_runtime_test.go`
- Modify: `go/internal/storage/cypher/*` only after the gate proves the writer
  owns the slowdown
- Modify: `go/cmd/ingester/wiring_canonical_writer.go` only if NornicDB writer
  mode defaults are proven by the same self-repo run
- Modify: `docs/docs/adrs/2026-05-07-dead-code-root-model-and-language-reachability.md`

- [ ] Add a gated local-authoritative canonical-write regression proof that
  captures a real self-repo run, not just synthetic query latency.
  -> verify: it reports `phase=files`, `phase=entities`, `Function` label
  rows/statements/executions/durations, and total projection duration.
- [x] Prove the slow path with no unrelated local owner process contending for
  CPU or graph writes.
  -> verify: process list shows no competing `eshu-ingester`, `eshu-reducer`,
  or `nornicdb-headless` for another workspace during the run.
- [x] Compare default file-scoped containment with
  `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true` using the same corpus and
  binary.
  -> verify: evidence table records both timings and marks any no-win as a
  rejected hypothesis.
- [x] Compare runtime mode against the nearest fast baseline.
  -> verify: embedded mode with `ESHU_NORNICDB_BINARY` still set restored
  `Function` grouped executions to millisecond averages, proving the slowdown
  came from sticky process-mode selection rather than dead-code metadata.
- [x] Fix the proven owner: local process-mode selection.
  -> verify: `TestUseProcessLocalNornicDBIgnoresBinaryWithoutProcessRuntime`
  failed before the fix and passed after; live graph proof succeeded in
  `19.42s`.
- [ ] Re-run live local-authoritative graph indexing and API/MCP dead-code proof.
  -> verify: self-repo projection returns to the previous envelope and the
  original false-positive list is not returned as actionable unused code.

## Language Track Template

Each language worker must produce the same shape:

- fixtures under `tests/fixtures/deadcode/<language>/`
- parser tests proving emitted root evidence, when the language supports it
- query tests proving classification and maturity behavior
- docs row updates in `tests/fixtures/deadcode/README.md`
- focused local tests that pass before integration

Every fixture directory should include a short `README.md` with expected
symbols:

- `unused`
- `direct_reference`
- `entrypoint`
- `public_api`
- `framework_root`
- `semantic_dispatch`
- `excluded`
- `ambiguous`

## Open-Source Dogfood Matrix

Use open-source dogfood after each language track has focused fixture coverage
and before promoting confidence language-wide. The point is not just to find a
large repo; it is to prove accuracy and performance on real project shapes that
exercise Eshu's parser and graph contracts.

| Track | Primary repos | Why these repos | Acceptance evidence |
| --- | --- | --- | --- |
| Java | Jenkins plus one additional Java service/tool | Large Maven layout, annotations, plugin callbacks, CLI helpers, nested packages | local-authoritative index reaches healthy queue-drained state; dead-code query returns bounded results; sample is manually triaged for live entrypoints, constructors, overrides, callbacks, and same-class calls |
| Python | Ansible plus one Python framework/tool repo | Python spread across libraries, CLI tools, plugins, tests, dataclasses, dynamic dispatch | local-authoritative index reaches healthy queue-drained state; dataclass and public API roots hold; dynamic-dispatch findings are classified as derived rather than cleanup-safe |
| Go | Helm, Kustomize, Kubernetes, Terraform | Popular Go CLIs with command wiring, interfaces, reflection, generated code, Kubernetes/Helm/Kustomize/Terraform artifacts | each repo indexes within the target envelope for its size; dead-code samples suppress known command roots and callback/DI helpers; parser relationship evidence remains useful for supported IaC surfaces |

Dogfood runs should record:

- repository commit or tag
- file/entity counts and skipped generated/vendor counts
- end-to-end index time and query latency
- candidate scan pages, rows, scan limit, and truncation state
- a short manual triage of at least ten returned candidates
- false positives that become fixtures before the language is considered done

## Language Track: Go

**Owner:** Go worker

**Files:**

- Create/modify: `tests/fixtures/deadcode/go/`
- Modify: `go/internal/parser/go_language.go`
- Modify: `go/internal/parser/go_dead_code_roots.go`
- Modify: `go/internal/parser/go_dead_code_registrations.go`
- Create: `go/internal/parser/go_dead_code_function_values_test.go`
- Create: `go/internal/parser/go_dead_code_interfaces_test.go`
- Modify: `go/internal/query/code_dead_code_dogfood_regression_test.go`
- Modify: `go/cmd/eshu/local_authoritative_dead_code_perf_test.go`

- [x] Honor explicit Go semantic root metadata in the query policy.
- [x] Add fixture cases for function values, method values, local interfaces,
  method-set satisfaction, DI callbacks, public APIs, tests, generated files,
  and reflection ambiguity.
  -> verify: `cd go && go test ./internal/parser -run 'GoDeadCode|FunctionValue|Interface' -count=1`.
- [x] Emit deterministic parser metadata for function values and local
  interface/method-set evidence.
  -> verify: parser tests fail first, then pass.
- [x] Extend query tests so Eshu dogfood false positives remain suppressed.
  -> verify: `cd go && go test ./internal/query -run TestDeadCodeDogfood -count=1`.

## Language Track: Python

**Owner:** Python worker

**Files:**

- Create/modify: `tests/fixtures/deadcode/python/`
- Modify: `go/internal/parser/python_language.go`
- Modify: `go/internal/parser/python_dead_code_roots.go`
- Modify: `go/internal/parser/python_dead_code_roots_test.go`
- Modify: `go/internal/query/code_dead_code_python_roots.go`
- Modify: `go/internal/query/code_dead_code_python_roots_test.go`

- [x] Add fixture cases for `__main__`, imports, package public surface,
  FastAPI, Flask, Celery, Click/Typer-style CLI roots, generated/test
  exclusions, and dynamic import ambiguity.
  -> verify: parser/query tests fail first.
- [x] Emit or preserve parser metadata for modeled decorators and CLI roots.
  -> verify: `cd go && go test ./internal/parser -run Python.*DeadCode -count=1`.
- [x] Classify Python results with maturity metadata and ambiguity notes.
  -> verify: `cd go && go test ./internal/query -run Python.*DeadCode -count=1`.

## Language Track: JavaScript

**Owner:** JS-family worker

**Files:**

- Create/modify: `tests/fixtures/deadcode/javascript/`
- Modify: `go/internal/parser/javascript_language.go`
- Modify: `go/internal/parser/javascript_dead_code_roots.go`
- Modify: `go/internal/parser/javascript_dead_code_roots_test.go`
- Modify: `go/internal/query/code_dead_code_javascript_roots.go`
- Modify: `go/internal/query/code_dead_code_javascript_roots_test.go`

- [x] Add fixture cases for ESM/CommonJS exports, direct calls, Express route
  handlers, generated/test exclusions, and dynamic property ambiguity.
  -> verify: JS dead-code tests fail first.
- [x] Emit root metadata for modeled Express and Next.js route roots.
  -> verify: `cd go && go test ./internal/parser -run JavaScript.*DeadCode -count=1`.
- [x] Classify JavaScript results with maturity metadata.
  -> verify: `cd go && go test ./internal/query -run JavaScript.*DeadCode -count=1`.

## Language Track: TypeScript

**Owner:** JS-family worker unless split into fixture-only subtask

**Files:**

- Create/modify: `tests/fixtures/deadcode/typescript/`
- Modify: `go/internal/parser/javascript_language.go`
- Modify: `go/internal/parser/javascript_typescript_semantics.go`
- Modify: `go/internal/parser/javascript_dead_code_roots.go`
- Modify: `go/internal/query/code_dead_code_typescript_semantics_test.go`

- [x] Add fixture cases for exported functions/classes/interfaces, decorators,
  Express/Next.js roots, generated/test exclusions, and dynamic import
  ambiguity.
  -> verify: TypeScript dead-code tests fail first.
- [x] Reuse JS-family parser root code without duplicating policy logic.
  -> verify: `cd go && go test ./internal/parser -run 'TypeScript.*DeadCode|JavaScript.*DeadCode' -count=1`.

## Language Track: TSX

**Owner:** JS-family worker unless split into fixture-only subtask

**Files:**

- Create/modify: `tests/fixtures/deadcode/tsx/`
- Modify: `go/internal/parser/javascript_language.go`
- Modify: `go/internal/parser/javascript_semantics.go`
- Modify: `go/internal/parser/javascript_dead_code_roots.go`

- [x] Add fixture cases for Next.js route exports, React component exports,
  hook ambiguity, generated/test exclusions, and unused local helpers.
  -> verify: `cd go && go test ./internal/parser -run 'TSX.*DeadCode|JavaScript.*DeadCode' -count=1`.

## Candidate-Only Language Tracks

These workers can run mostly in parallel because they should start with
fixtures, maturity assertions, and parser evidence tests. Do not promote to
`derived` until the language has at least one modeled non-trivial root.

| Language | Fixture path | Parser files | Focused verification |
| --- | --- | --- | --- |
| C | `tests/fixtures/deadcode/c/` | `go/internal/parser/c_language.go` | `cd go && go test ./internal/parser -run C.*DeadCode -count=1` |
| C++ | `tests/fixtures/deadcode/cpp/` | `go/internal/parser/cpp_language.go` | `cd go && go test ./internal/parser -run Cpp.*DeadCode -count=1` |
| C# | `tests/fixtures/deadcode/csharp/` | `go/internal/parser/csharp_language.go` | `cd go && go test ./internal/parser -run CSharp.*DeadCode -count=1` |
| Dart | `tests/fixtures/deadcode/dart/` | `go/internal/parser/dart_language.go` | `cd go && go test ./internal/parser -run Dart.*DeadCode -count=1` |
| Elixir | `tests/fixtures/deadcode/elixir/` | `go/internal/parser/elixir_language.go` | `cd go && go test ./internal/parser -run Elixir.*DeadCode -count=1` |
| Groovy | `tests/fixtures/deadcode/groovy/` | `go/internal/parser/groovy_language.go` | `cd go && go test ./internal/parser -run Groovy.*DeadCode -count=1` |
| Haskell | `tests/fixtures/deadcode/haskell/` | `go/internal/parser/perl_haskell_language.go` | `cd go && go test ./internal/parser -run Haskell.*DeadCode -count=1` |
| Java | `tests/fixtures/deadcode/java/` | `go/internal/parser/java_language.go` | `cd go && go test ./internal/parser -run Java.*DeadCode -count=1` |
| Kotlin | `tests/fixtures/deadcode/kotlin/` | `go/internal/parser/kotlin_language.go` | `cd go && go test ./internal/parser -run Kotlin.*DeadCode -count=1` |
| Perl | `tests/fixtures/deadcode/perl/` | `go/internal/parser/perl_haskell_language.go` | `cd go && go test ./internal/parser -run Perl.*DeadCode -count=1` |
| PHP | `tests/fixtures/deadcode/php/` | `go/internal/parser/php_language.go` | `cd go && go test ./internal/parser -run PHP.*DeadCode -count=1` |
| Ruby | `tests/fixtures/deadcode/ruby/` | `go/internal/parser/ruby_language.go` | `cd go && go test ./internal/parser -run Ruby.*DeadCode -count=1` |
| Rust | `tests/fixtures/deadcode/rust/` | `go/internal/parser/rust_language.go` | `cd go && go test ./internal/parser -run Rust.*DeadCode -count=1` |
| Scala | `tests/fixtures/deadcode/scala/` | `go/internal/parser/scala_language.go` | `cd go && go test ./internal/parser -run Scala.*DeadCode -count=1` |
| Swift | `tests/fixtures/deadcode/swift/` | `go/internal/parser/swift_language.go` | `cd go && go test ./internal/parser -run Swift.*DeadCode -count=1` |

For each candidate-only language:

- [x] Create fixture README and source fixture files.
- [ ] Add parser evidence tests that mirror fixture intent.
- [x] Add query maturity tests asserting `derived_candidate_only`.
- [ ] Leave exactness unpromoted until root and reachability gates exist.

## Integration Gate Before Local Graph Restart

Run these before asking the user to restart Eshu graph:

```bash
cd go && go test ./internal/parser ./internal/query ./internal/mcp ./cmd/eshu -count=1
cd go && go test ./internal/facts ./internal/storage/postgres ./internal/reducer -run DeadCode -count=1
cd go && go test ./internal/telemetry -run DeadCode -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

If those pass, rebuild local binaries:

```bash
cd go
go build -o ./bin/eshu ./cmd/eshu
go build -o ./bin/eshu-api ./cmd/api
go build -o ./bin/eshu-ingester ./cmd/ingester
go build -o ./bin/eshu-reducer ./cmd/reducer
export PATH="$PWD/bin:$PATH"
```

Then ask the user to restart Eshu graph for live proof.

## Local Graph Proof After Restart

After the user restarts Eshu graph:

- [ ] Confirm graph owner is healthy.
  -> verify: `eshu graph status --workspace-root "$PWD"`.
- [ ] Confirm API health.
  -> verify: `curl -fsS "$ESHU_SERVICE_URL/healthz"` if a URL is exported, or
  use the owner record URL.
- [ ] Run MCP/API dead-code query against this repo.
  -> verify: `find_dead_code` returns language maturity metadata and does not
  return the Eshu dogfood false positives as actionable unused results.
- [ ] Run graph-analysis compose proof.
  -> verify: `./scripts/verify_graph_analysis_compose.sh`.

## Final Verification

Run:

```bash
cd go && go test ./internal/parser ./internal/facts ./internal/storage/postgres ./internal/reducer ./internal/query ./internal/mcp ./cmd/eshu -count=1
cd go && go test ./internal/backendconformance -run DeadCode -count=1
ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless ESHU_LOCAL_AUTHORITATIVE_PERF=true go test ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
./scripts/verify_graph_analysis_compose.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
