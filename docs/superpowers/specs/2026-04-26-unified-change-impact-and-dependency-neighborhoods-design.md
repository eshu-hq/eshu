# Unified Change Impact And Dependency Neighborhoods Design

**Date:** 2026-04-26  
**Status:** Draft for review  
**Related ADR:** `docs/docs/adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`  
**Companion spec:** `docs/superpowers/specs/2026-04-26-cross-repo-contract-bridge-and-service-dependency-graph-design.md`
**Implementation plan:** `docs/superpowers/plans/2026-04-26-unified-change-impact-and-dependency-neighborhoods-implementation.md`

## Summary

Create one shared Eshu capability for answering dependency and change-impact
questions across code, IaC, deployment, and runtime context. The core contract is
`graph.neighborhood`: given a selected file, symbol, IaC artifact, deployment
root, manifest, workload, or runtime resource, return what it depends on, what
depends on it, how it reaches roots or workloads, what findings apply, and what
truth limits the answer.

The interactive UI is one consumer of this contract. The same response must also
serve MCP, CLI, PR automation, refactor tooling, cleanup workflows, and internal
platform APIs.

## Goals

1. Define a unified dependency-neighborhood response shape shared by HTTP, MCP,
   CLI, UI, and automation consumers.
2. Add a change-impact workflow that starts from a selected entity or git diff
   and returns direct impact, transitive paths, findings, blast radius, and
   explainable risk.
3. Preserve Eshu truth labels and unsupported-capability envelopes instead of
   hiding partial coverage behind simplified graph output.
4. Replace UI-specific or raw-Cypher dependency logic with documented query
   surfaces.
5. Make the first implementation useful with the code and IaC relationships Eshu
   already has, while leaving room for richer edge families later.

## Non-Goals

1. Do not build a new graph storage model for this feature.
2. Do not replace existing domain-specific code, IaC, impact, or content routes.
3. Do not claim exact change safety when graph coverage is partial or derived.
4. Do not require live cluster, cloud, or Terraform plan execution for the first
   implementation.
5. Do not make UI behavior the source of truth for the response contract.

## Current Flow

Eshu already exposes several pieces of this workflow:

1. Code relationship queries return callers, callees, imports, importers, and
   dead-code analysis for indexed source entities.
2. Content-backed relationship builders expose IaC and delivery relationships
   for Terraform, Kustomize, ArgoCD, Helm, Kubernetes, GitHub Actions,
   Dockerfile, and Compose evidence.
3. Impact and deployment-trace routes connect services, workflows, deploy
   sources, workloads, and runtime context.
4. The previous VS Code extension prototype is removed from the repo; future
   editor surfaces should be rebuilt from the shared graph model instead of
   becoming the canonical product surface.

The missing piece is a response shape that composes these signals into one
selected-entity view and one diff-aware impact workflow.

## Problem Statement

Users rarely ask for an isolated graph edge. They ask:

1. "What does this file depend on?"
2. "Who depends on this module?"
3. "What breaks if I rename this values file?"
4. "What services or environments can this PR affect?"
5. "Is this unused, unreachable, or just ambiguous?"

Today those answers can require multiple route families and domain-specific
knowledge. That creates duplicated client logic, makes MCP answers harder to
trust, and encourages UI clients to query storage directly. Eshu needs one
composition layer that keeps the typed evidence and truth labels intact.

## Design

### 1. Shared Neighborhood Contract

Add a graph-neighborhood query capability with the candidate route:

```text
POST /api/v0/graph/neighborhood
```

The request accepts these selectors:

1. `entity_id` for canonical graph entities.
2. `repo_id + path` for files and artifacts.
3. `repo_id + path + line/range` for source-position selection.
4. `repo_id + semantic_name + kind` for fallback lookup.
5. Optional `max_depth`, `direction`, `relationship_types`, and
   `include_content_handles` controls.

The response includes:

1. `subject`: identity, kind, repo, path, line/range, display label.
2. `incoming`: direct callers, importers, consumers, roots, owners, and
   reverse references.
3. `outgoing`: direct dependencies, calls, imports, referenced artifacts,
   deployment refs, and runtime refs.
4. `paths`: bounded transitive paths to roots, services, workloads,
   environments, and runtime resources.
5. `findings`: dead-code, dead-IaC, integrity, unresolved-reference, and
   ambiguous-dynamic findings attached to the subject.
6. `blast_radius`: affected repos, services, workloads, environments, cloud
   resources, runtime resources, and contracts.
7. `risk_summary`: explainable risk level, reasons, unknowns, and next steps.
8. `truth`: exactness, partial coverage, unsupported capabilities, and source
   evidence limitations.

The query layer should compose existing graph, content, and impact readers
through ports. It must not depend on a concrete graph adapter or require clients
to know which domain-specific route owns each edge family.

### 2. Change Impact Workflow

Add a change-impact workflow that can start from either an explicit subject or a
git diff.

Subject mode:

```text
selected entity -> neighborhood -> paths -> findings -> risk summary
```

Diff mode:

```text
changed files/ranges -> resolved subjects -> merged neighborhoods -> impact
summary -> review findings
```

Diff input must support staged, unstaged, all, and compare/base-ref scopes. When
the input is a unified diff, the first implementation should parse zero-context
hunks and resolve impacted entities by explicit line-range overlap:

```text
entity.start_line <= hunk.end && entity.end_line >= hunk.start
```

File-only fallback is allowed for deletes, renames, binary files, untracked
files, or missing source spans, but the response must label those cases as
partial or derived. Large diffs must be bounded, and skipped files must be
reported rather than silently ignored.

The diff workflow should classify changes into:

1. source code entities
2. IaC artifacts and definitions
3. deployment config and controller roots
4. contract/schema artifacts
5. unknown files that still need source evidence

The merged response should deduplicate shared downstream paths and report when
multiple changed subjects affect the same service, workload, environment, or
contract.

### 3. Explainable Risk

Risk must be derived and auditable, not a black-box score. The first model uses
ordered reasons:

1. production or runtime root reachability
2. public or cross-repo contract consumers
3. number and diversity of direct dependents
4. transitive paths to services, workloads, environments, and cloud resources
5. attached findings such as missing paths or unresolved references
6. unknown coverage that prevents exactness

The response should use levels such as `low`, `medium`, `high`, and `unknown`.
`unknown` is valid when coverage is too incomplete to make a defensible risk
claim.

### 4. Consumer Surfaces

The same response should power:

1. MCP: `get_dependency_neighborhood` and a future diff-aware impact tool.
2. CLI: `eshu analyze neighborhood` and `eshu analyze impact --diff`.
3. UI: selected-node graph, file tree, source inspector, incoming/outgoing,
   paths, findings, and blast-radius panes.
4. PR automation: changed-file impact summaries and review annotations.
5. Refactor tooling: rename, move, delete, and extraction preflight checks.
6. Cleanup workflows: dead-code and dead-IaC evidence review.

Agent and automation consumers should treat the route as a guardrail: inspect
neighborhood or impact before symbol, file, contract, or IaC edits, and run
diff-aware impact before commit or PR. High, unknown, stale, or partial risk
should be visible in the response so agents do not proceed from a false sense of
safety.

### 5. Telemetry

Add query telemetry that answers:

1. how often neighborhood and diff-impact queries run
2. how many subjects, edges, paths, and findings they return
3. how often responses are truncated
4. how often unsupported capabilities or partial coverage affect answers
5. which resolver stage failed without using high-cardinality metric labels

Paths and identifiers belong in spans, structured logs, and response evidence,
not metric labels.

## Proposed Components

1. `go/internal/query/graph_neighborhood.go`: request handling and response
   assembly.
2. `go/internal/query/change_impact.go`: explicit-subject and diff-driven
   impact composition.
3. `go/internal/query/neighborhood_ports.go`: graph, content, relationship,
   finding, and impact reader ports.
4. `go/internal/mcp`: `get_dependency_neighborhood` routing.
5. `go/cmd/eshu`: CLI wrappers for neighborhood and diff impact.
6. Future editor or web UI: consume the documented route instead of raw
   dependency logic.
7. Docs: HTTP, MCP, CLI, and UI behavior references.

## Error Handling

1. Ambiguous selector matches return structured candidates, not arbitrary
   first-match results.
2. Missing graph support returns `unsupported_capability` with the unavailable
   relationship family.
3. Partial indexed coverage returns a partial response plus limitations.
4. Truncated paths report truncation counts and depth limits.
5. Source-content misses keep graph edges visible and mark content evidence as
   unavailable.
6. Reader leg failures, timeouts, stale generations, unsupported diff inputs,
   and skipped high-fanout expansions return structured partial results with an
   error class and affected response section.

## Testing Strategy

1. Query tests for code-only neighborhoods.
2. Query tests for IaC-only neighborhoods.
3. Query tests for mixed code-to-IaC-to-runtime paths.
4. Diff-impact tests for changed source and changed IaC files.
5. Ambiguity tests for duplicate names and missing selectors.
6. Unsupported-capability tests for lightweight or partial graph modes.
7. MCP and CLI routing tests.
8. UI contract tests proving no raw storage query is required for dependency
   panels.
9. Compose or local-authoritative proof before documenting exact graph-backed
   impact.
10. Concurrency tests for bounded fan-out, context cancellation, and no
    cross-leg response mutation races.
11. Telemetry tests that prove operators can see truncation, partial coverage,
    stale generation, ambiguity, timeout, and unsupported capability rates.

## Rollout

### Phase 1: Response Contract And Code Relationships

Ship `POST /api/v0/graph/neighborhood` for existing code relationships and
basic file/source evidence.

### Phase 2: IaC And Deployment Relationships

Fold in IaC usage, reachability, integrity, and deployment-root evidence from
the related ADR.

### Phase 3: Diff And PR Impact

Add git diff input, changed-subject resolution, merged blast radius, and PR
automation output.

### Phase 4: UI And Refactor Tooling

Move UI and refactor clients onto the documented route and remove raw
storage-specific dependency logic.

## Open Decisions

1. Whether the first diff-impact API should accept raw diff text, a commit
   range, or a pre-resolved changed-file list.
2. Whether risk levels should be emitted by the base query route or a separate
   impact route that consumes neighborhood output.
3. Whether UI source previews should use content handles only or allow embedded
   snippets in small responses.
