# Unified Change Impact And Dependency Neighborhoods Implementation Plan

> **For agentic workers:** REQUIRED: Use `superpowers:executing-plans` to implement this plan task-by-task. Use `karpathy-guidelines` and `google-go-best-practices` for every Go change.

**Goal:** Ship a shared `graph.neighborhood` and change-impact capability that serves API, MCP, CLI, PR automation, refactor tooling, cleanup workflows, and UI consumers.

**Architecture:** Add a query composition layer over existing code, content, impact, and future contract readers. Keep handlers small, define consumer-side ports in `go/internal/query`, and return truth labels, limitations, and truncation metadata for every partial answer.

**Tech Stack:** Go HTTP handlers, query ports, MCP dispatch, Cobra CLI, OTEL telemetry, MkDocs

**Related spec:** `docs/superpowers/specs/2026-04-26-unified-change-impact-and-dependency-neighborhoods-design.md`
**Related ADR:** `docs/docs/adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`

---

## Core Goals

1. **Accuracy:** selectors must resolve deterministically or return candidates; no arbitrary first match.
2. **Concurrency:** query fan-out must be bounded by context cancellation and depth limits.
3. **Speed:** reuse existing graph/content readers, cap path expansion, and report truncation.
4. **Instrumentation:** emit query duration, edge counts, path counts, truncation counts, and unsupported-capability counts.

## Rigor Addendum

**Current flow:** selectors enter through API, MCP, CLI, VS Code, or PR automation; query code resolves subjects against facts, graph, and content readers; graph/content/impact legs fan out; responses return truth labels, partial coverage, truncation, and limitations to all consumers.

**Shared state:** canonical graph projections, content relationship indexes, source spans, repository generations, OpenAPI schemas, telemetry instruments, and later contract/IaC fact tables.

**Transaction and retry boundaries:** neighborhood reads are read-only and request-scoped; change-impact diff parsing is local to the request; reducer-owned materialization remains outside this plan. Retries belong at the caller boundary and must not hide unsupported graph capabilities or stale generations.

**Deadlock, race, and ordering hazards:** avoid query-time writes, unbounded goroutine fan-out, response mutation from multiple workers, stale graph/content generation mixing without labels, and opposite-order reads that make one leg wait on another. Preserve concurrency by partitioning fan-out by leg and depth while sharing only immutable request state.

**Edge cases:** ambiguous selectors, deleted files, renamed files, binary diffs, untracked files, empty diffs, missing line spans, unsupported graph adapters, high-fanout nodes, timeout/cancellation, and partial reader failure.

**Observability contract:** operators must see query latency, leg latency, fanout, truncation, partial coverage, stale generation, ambiguity, timeout, unsupported capability, and error class without path, symbol, route, or topic labels.

## Task 1: Define Neighborhood Request And Response Contracts

**Files:**
- Create: `go/internal/query/graph_neighborhood_types.go`
- Create: `go/internal/query/graph_neighborhood_types_test.go`
- Modify: `docs/docs/reference/http-api.md`

1. Write failing table tests for entity, repo/path, repo/path/line, and semantic-name selectors.
   -> verify: `cd go && go test ./internal/query -run 'TestGraphNeighborhood.*Contract' -count=1` fails.
2. Implement minimal request/response structs with exported Go doc comments, lowercase error strings, and validation errors that identify the invalid field.
   -> verify: focused contract tests pass.
3. Document JSON request/response fields.
   -> verify: strict MkDocs build passes.
4. Commit.
   -> verify: worktree clean after commit.

## Task 2: Implement Subject Resolution Ports

**Files:**
- Create: `go/internal/query/graph_neighborhood_ports.go`
- Create: `go/internal/query/graph_neighborhood_resolution.go`
- Create: `go/internal/query/graph_neighborhood_resolution_test.go`
- Modify: `go/internal/query/entity.go`
- Modify: `go/internal/query/code_relationships.go`

1. Write failing tests for exact entity ID resolution, repo/path file resolution, line-to-entity resolution, ambiguous semantic names, and missing subjects.
   -> verify: focused resolution tests fail for missing implementation.
2. Implement consumer-side interfaces for graph, content, relationship, and impact readers.
   -> verify: tests pass without concrete adapter imports in the handler.
3. Ensure context-aware calls accept `context.Context` as the first parameter and stop work on cancellation.
   -> verify: cancellation test returns before child lookups complete.
4. Commit.
   -> verify: `cd go && go test ./internal/query -run 'TestGraphNeighborhood.*Resolution' -count=1`.

## Task 3: Compose Incoming, Outgoing, Paths, Findings, And Blast Radius

**Files:**
- Create: `go/internal/query/graph_neighborhood.go`
- Create: `go/internal/query/graph_neighborhood_test.go`
- Modify: `go/internal/query/code_relationships.go`
- Modify: `go/internal/query/content_relationships.go`
- Modify: `go/internal/query/impact.go`

1. Write failing tests for code-only incoming/outgoing relationships using existing relationship fixtures.
   -> verify: graph-neighborhood tests fail.
2. Add content-backed IaC relationship composition while preserving truth basis and limitations.
   -> verify: tests cover graph-backed and content-backed responses.
3. Add bounded path expansion with `max_depth`, duplicate suppression, and truncation metadata.
   -> verify: tests assert depth cap and truncation counts.
4. Add blast-radius fields only when evidence exists; unknown coverage must be explicit.
   -> verify: tests assert no fabricated service/workload impact.
5. Return structured partial results when one leg fails or times out, including `partial`, `limitations`, `error_class`, and per-leg status.
   -> verify: tests cover graph reader failure, content reader failure, timeout, and unsupported capability envelopes.
6. Commit.
   -> verify: `cd go && go test ./internal/query -run 'TestGraphNeighborhood' -count=1`.

## Task 4: Add HTTP Route, OpenAPI, And Telemetry

**Files:**
- Modify: `go/internal/query/code.go` or create `go/internal/query/graph.go`
- Modify: `go/internal/query/openapi.go`
- Create: `go/internal/query/openapi_paths_graph.go`
- Modify: `go/internal/query/openapi_test.go`
- Modify: `go/internal/telemetry/instruments.go`
- Modify: `go/internal/telemetry/instruments_test.go`
- Modify: `go/internal/telemetry/contract.go`
- Modify: `go/internal/telemetry/contract_test.go`

1. Write failing handler tests for `POST /api/v0/graph/neighborhood`.
   -> verify: route test returns 404 or missing route before implementation.
2. Mount the route and return `WriteSuccess` with the correct truth envelope.
   -> verify: handler tests pass.
3. Add OpenAPI path and schema coverage.
   -> verify: `cd go && go test ./internal/query -run 'TestServeOpenAPI|TestGraphNeighborhood' -count=1`.
4. Add low-cardinality query telemetry for latency, leg latency, fanout, truncation, partial coverage, stale generation, ambiguity, timeout, and unsupported capability.
   -> verify: telemetry tests assert metric names and exclude path, symbol, route, and topic labels.
5. Commit.
   -> verify: focused query tests pass after commit.

## Task 5: Add Change Impact From Subject And Diff Inputs

**Files:**
- Create: `go/internal/query/change_diff.go`
- Create: `go/internal/query/change_diff_test.go`
- Create: `go/internal/query/change_impact.go`
- Create: `go/internal/query/change_impact_test.go`
- Modify: `go/internal/query/openapi_paths_impact.go`
- Modify: `docs/docs/reference/http-api.md`

1. Write failing tests for explicit-subject impact, unified diff hunk parsing, and diff input classification.
   -> verify: `cd go && go test ./internal/query -run 'TestChangeImpact' -count=1` fails.
2. Parse unified diffs generated with zero context, supporting unstaged, staged, all, and compare/base-ref scopes from API or CLI callers.
   -> verify: tests cover added, modified, deleted, renamed, binary, untracked, and empty diff inputs.
3. Map changed hunks to subjects with explicit line-overlap semantics: `entity.start_line <= hunk.end && entity.end_line >= hunk.start`.
   -> verify: tests prove boundary overlaps, deleted-line handling, file-only fallback, and missing-span limitations.
4. Implement changed-file classification for source, IaC, deployment config, contract/schema, and unknown files.
   -> verify: classification tests pass.
5. Enforce bounded diff size, skipped file reporting, and structured partial responses for unsupported inputs.
   -> verify: tests assert `partial`, `skipped_files`, `limitations`, and `error_class` values.
6. Merge multiple subject neighborhoods, deduplicate shared paths, and compute explainable risk.
   -> verify: tests assert stable risk reasons and no black-box score.
7. Add OpenAPI and docs.
   -> verify: strict docs and focused OpenAPI tests pass.
8. Commit.
   -> verify: focused tests pass after commit.

## Task 6: Add MCP And CLI Consumers

**Files:**
- Modify: `go/internal/mcp/dispatch.go`
- Modify: `go/internal/mcp/tools_codebase.go`
- Modify: `go/internal/mcp/dispatch_test.go`
- Modify: `go/cmd/eshu/analyze.go`
- Modify: `go/cmd/eshu/analyze_test.go`
- Modify: `docs/docs/reference/mcp-reference.md`
- Modify: `docs/docs/reference/cli-reference.md`
- Modify: `docs/docs/guides/mcp-guide.md`
- Modify: `docs/docs/guides/starter-prompts.md`

1. Write failing MCP dispatch tests for `get_dependency_neighborhood` and a diff-aware impact tool.
   -> verify: MCP tests fail on missing tools/routes.
2. Add `eshu analyze neighborhood` and `eshu analyze impact --diff` command tests.
   -> verify: CLI tests fail before command registration.
3. Implement route mappings and CLI wrappers with JSON passthrough.
   -> verify: `cd go && go test ./internal/mcp ./cmd/eshu -run 'Neighborhood|Impact' -count=1`.
4. Document agent guardrails: run neighborhood or impact before symbol/IaC edits, run diff-aware impact before commit or PR, warn on high or unknown risk, and avoid raw find/replace for rename workflows.
   -> verify: docs explain the workflow without weakening existing truth-label requirements.
5. Commit.
   -> verify: worktree clean after commit.

## Task 7: Rebuild Editor Dependency Panels From The Shared Route

**Files:**
- Future editor client package
- Future editor client README

1. Write failing TypeScript tests or mocks proving dependency panels call `/api/v0/graph/neighborhood` instead of raw Cypher/import-only logic.
   -> verify: editor client test command fails before implementation.
2. Implement a typed client response with no `any` unless explicitly justified in a comment.
   -> verify: TypeScript build and focused tests pass.
3. Preserve existing import display as a subset of the new response.
   -> verify: regression tests pass.
4. Commit.
   -> verify: extension test/build command and docs build pass.

## Final Verification

Run:

```bash
cd go && go test ./internal/query ./internal/mcp ./cmd/eshu -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

If a future editor client task changes TypeScript, also run that package's
documented test/build command and report the exact command.
