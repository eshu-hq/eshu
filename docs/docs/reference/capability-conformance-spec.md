# Capability Conformance Spec

This document defines the capability matrix that governs lightweight local,
full local stack, and production query behavior.

It exists for two reasons:

- to keep the product contract explicit
- to give Chunks 2 and 5 a machine-readable source for tests and backend
  conformance runs
- to keep truth labels aligned with the wire contract defined in
  `truth-label-protocol.md`

The canonical machine-readable source for this matrix lives at:

- `specs/capability-matrix.v1.yaml`

## Why This Exists

Eshu cannot rely on prose like "local mode should be good at search" or
"production is authoritative."

The runtime needs one explicit matrix that says:

- which capability is being discussed
- which profile is being evaluated
- whether the capability is supported
- what truth level is allowed
- what latency envelope is expected
- what scope size the claim applies to
- which verification gate proves it

Without that matrix, backend experiments and local-mode work will drift into
undocumented differences.

## Capability IDs

Capability families are grouped by user intent, not storage backend. That keeps
the contract stable even if backend implementations change.

The initial capability families are:

- `code_search.exact_symbol`
- `code_search.fuzzy_symbol`
- `code_search.variable_lookup`
- `code_search.content_search`
- `symbol_graph.decorators`
- `symbol_graph.argument_names`
- `symbol_graph.class_methods`
- `symbol_graph.imports`
- `symbol_graph.inheritance`
- `code_quality.complexity`
- `call_graph.direct_callers`
- `call_graph.direct_callees`
- `call_graph.transitive_callers`
- `call_graph.transitive_callees`
- `call_graph.call_chain_path`
- `code_quality.dead_code`
- `platform_impact.deployment_chain`
- `platform_impact.context_overview`
- `relationship_evidence.drilldown`
- `platform_impact.resource_to_code`
- `platform_impact.resource_investigation`
- `platform_impact.dependency_path`
- `platform_impact.environment_compare`
- `platform_impact.change_surface`
- `platform_impact.blast_radius`

## MCP Tool To Capability Mapping

The same capability ID may back more than one surface or tool. Initial mapping:

| MCP tool | Primary capability IDs |
| --- | --- |
| `find_code` | `code_search.exact_symbol`, `code_search.fuzzy_symbol` |
| `search_file_content` | `code_search.content_search` |
| `analyze_code_relationships` | `call_graph.direct_callers`, `call_graph.direct_callees`, `call_graph.transitive_callers`, `call_graph.transitive_callees`, `symbol_graph.class_methods`, `symbol_graph.inheritance` |
| `find_function_call_chain` | `call_graph.call_chain_path` |
| `find_dead_code` | `code_quality.dead_code` |
| `calculate_cyclomatic_complexity` | `code_quality.complexity` |
| `find_most_complex_functions` | `code_quality.complexity` |
| `find_blast_radius` | `platform_impact.blast_radius` |
| `find_change_surface` | `platform_impact.change_surface` |
| `investigate_change_surface` | `platform_impact.change_surface` |
| `trace_deployment_chain` | `platform_impact.deployment_chain` |
| `investigate_deployment_config` | `platform_impact.deployment_config_influence` |
| `find_infra_resources` | `platform_impact.deployment_chain` |
| `analyze_infra_relationships` | `platform_impact.deployment_chain` |
| `get_repo_context` | `platform_impact.context_overview`, `relationship_evidence.drilldown` |
| `get_relationship_evidence` | `relationship_evidence.drilldown` |
| `build_evidence_citation_packet` | `evidence_citation.packet` |
| `get_service_context` | `platform_impact.context_overview` |
| `get_ecosystem_overview` | `platform_impact.context_overview` |
| `execute_language_query` | `symbol_graph.decorators`, `symbol_graph.argument_names`, `symbol_graph.class_methods`, `symbol_graph.imports`, `symbol_graph.inheritance` |
| `trace_resource_to_code` | `platform_impact.resource_to_code` |
| `investigate_resource` | `platform_impact.resource_investigation` |
| `explain_dependency_path` | `platform_impact.dependency_path` |
| `compare_environments` | `platform_impact.environment_compare` |

If a tool maps to multiple capability IDs, the response should identify the
capability actually exercised.

Some existing content, context, and runtime/admin tools remain outside the
initial local-codeintelligence capability matrix. They continue to be governed
by their existing runtime and API contracts until they are promoted into this
matrix explicitly.

## Profiles

Supported profile IDs:

- `local_lightweight`
- `local_authoritative`
- `local_full_stack`
- `production`

`local_authoritative` runs the local Eshu service plus embedded NornicDB
by default (see `graph-backend-installation.md`). It unlocks the
high-authority capabilities that `local_lightweight` refuses, without
requiring Docker Compose.

## Runtime Execution Modes

`required_runtime` is a separate axis from `profile`.

Allowed values:

- `local_host`
- `local_host_plus_graph`
- `full_stack`
- `deployed_services`

## Graph Backends

`graph_backend` is a separate axis from both `profile` and `required_runtime`.
It records which graph-adapter implementation served a response. This axis is
surfaced in telemetry and, optionally, in `truth.backend` on responses.

Allowed values:

- `neo4j`
- `nornicdb`

Default today is `nornicdb`. Neo4j remains the explicit official alternative.
Evaluation and release-backed adoption criteria for NornicDB live in
`docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
`local_lightweight` does not bind a graph backend because it refuses
graph-backed capabilities.

## Allowed Status Values

- `supported`
- `unsupported`
- `experimental`

## Allowed Verification Types

- `go_test`
- `integration_test`
- `compose_e2e`
- `remote_validation`

## Allowed Truth Levels

- `exact`
- `derived`
- `fallback`
- `unsupported`

`unsupported` is used in the matrix even though runtime responses use a
structured error instead of a truth label. This allows the same machine-readable
matrix to drive both capability gating and test expectations.

## Allowed Scope Sizes

- `active_repo`
- `active_monofolder`
- `indexed_workspace`
- `multi_repo_platform`

## Matrix Contract

The canonical YAML uses one entry per capability, with per-profile behavior
nested under `profiles`.

Each capability entry must provide:

- `capability`
- `tools`
- `profiles`

`tools` are declared per capability, not per profile. That means one MCP tool
may expose the same capability across multiple profiles while still returning a
different truth level, latency envelope, or support status in each profile.

Each `profiles.<profile_id>` object must provide:

- `status`
- `max_truth_level`
- `required_runtime`
- `p95_latency_ms`
- `max_scope_size`
- `verification`
- `notes`

`p95_latency_ms: null` means "not applicable because the capability is
unsupported for that profile."

## Backend Conformance Matrix

Profile capabilities and backend capabilities are related, but they answer
different questions.

- `specs/capability-matrix.v1.yaml` says what a user-facing capability may
  claim in each runtime profile.
- `specs/backend-conformance.v1.yaml` says which graph-backend behavior classes
  each official adapter currently supports.

The backend matrix tracks:

- canonical writes
- direct graph reads
- bounded path traversal
- graph-native full-text support
- dead-code readiness
- performance envelope evidence

NornicDB is the default backend, and Neo4j is the official alternative. Both
must stay listed in the backend matrix for as long as they are official Eshu
graph backends. The default test harness lives in
`go/internal/backendconformance`; it validates the backend matrix and runs
DB-free read/write corpora that live integration tests can reuse against
actual Neo4j or NornicDB instances.

## Rules

1. If a row is `supported`, the runtime must not exceed `max_truth_level`.
2. If a row is `unsupported`, the runtime must return a structured
   unsupported-capability error rather than silently degrading.
3. `experimental` rows must never be described as production-ready in docs.
4. Backend conformance can only elevate a graph adapter after the matrix passes
   for the intended profile.
5. Matrix rows must stay aligned with the envelope semantics in
   `truth-label-protocol.md`.
6. `status: unsupported` maps to `error.code=unsupported_capability` in the
   wire contract.

## Example Matrix Slice

```yaml
- capability: code_search.exact_symbol
  tools:
    - find_code
  profiles:
    local_lightweight:
      status: supported
      max_truth_level: exact
      required_runtime: local_host
      p95_latency_ms: 500
      max_scope_size: active_repo
      verification:
        - go_test: ./internal/query
      notes: Indexed entities and relational lookup tables are sufficient.
    local_full_stack:
      status: supported
      max_truth_level: exact
      required_runtime: full_stack
      p95_latency_ms: 500
      max_scope_size: indexed_workspace
      verification:
        - go_test: ./internal/query
      notes: Indexed entities and relational lookup tables are sufficient.

- capability: call_graph.transitive_callers
  tools:
    - analyze_code_relationships
  profiles:
    local_lightweight:
      status: unsupported
      max_truth_level: unsupported
      required_runtime: local_host
      p95_latency_ms: null
      max_scope_size: active_repo
      verification:
        - integration_test: lightweight-local-unsupported-transitive-callers
      notes: Lightweight local mode must not fake authoritative transitive truth.
    local_authoritative:
      status: supported
      max_truth_level: exact
      required_runtime: local_host_plus_graph
      p95_latency_ms: 2000
      max_scope_size: active_repo
      verification:
        - integration_test: local-authoritative-transitive-callers
      notes: Authoritative local graph mode.
    local_full_stack:
      status: supported
      max_truth_level: exact
      required_runtime: full_stack
      p95_latency_ms: 3000
      max_scope_size: indexed_workspace
      verification:
        - compose_e2e: transitive-callers
      notes: Authoritative graph mode.

- capability: platform_impact.context_overview
  tools:
    - get_repo_context
    - get_service_context
    - get_ecosystem_overview
  profiles:
    local_lightweight:
      status: unsupported
      max_truth_level: unsupported
      required_runtime: local_host
      p95_latency_ms: null
      max_scope_size: active_monofolder
      verification:
        - integration_test: local-lightweight-unsupported-context-overview
      notes: Requires full platform topology and deployed context truth.
    local_authoritative:
      status: supported
      max_truth_level: exact
      required_runtime: local_host_plus_graph
      p95_latency_ms: 4000
      max_scope_size: indexed_workspace
      verification:
        - go_test: ./internal/query
      notes: Authoritative local graph context for the indexed workspace.
    local_full_stack:
      status: supported
      max_truth_level: exact
      required_runtime: full_stack
      p95_latency_ms: 4000
      max_scope_size: indexed_workspace
      verification:
        - compose_e2e: context-overview
      notes: Exact only when repository, service, and ecosystem context agree.
```

## Test Responsibilities

### Chunk 2

Use the matrix to verify that extracted capability ports preserve the currently
allowed response behavior for each profile.

### Chunk 5

Use the capability matrix and backend matrix as baseline conformance harness
inputs for any backend under evaluation.

## Change Policy

Changing the matrix is a product contract change. Every matrix edit must
include:

- the updated machine-readable YAML
- matching docs if user-visible behavior changed
- a verification update for the affected profile/capability pair

Capability lifecycle rules:

- add
  - introduce a new ID and add matrix rows for every supported profile
- deprecate
  - keep the ID in the matrix with a deprecation note for at least one release
- remove
  - only after the deprecation window and client-facing docs have been updated
