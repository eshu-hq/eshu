# Cross-Repo Contract Bridge And Service Dependency Graph Design

**Date:** 2026-04-26  
**Status:** Draft for review  
**Related ADR:** `docs/docs/adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`  
**Companion spec:** `docs/superpowers/specs/2026-04-26-unified-change-impact-and-dependency-neighborhoods-design.md`
**Implementation plan:** `docs/superpowers/plans/2026-04-26-cross-repo-contract-bridge-and-service-dependency-graph-implementation.md`

## Summary

Add a contract-aware relationship layer that discovers providers and consumers
across repositories, links them through stable contract identities, and feeds
those links into Eshu dependency neighborhoods and change-impact workflows.

This follows the useful GitNexus idea of a cross-repo contract bridge, but keeps
Eshu's typed evidence, truth labels, and code-to-cloud scope. Contract edges are
not just UI graph links; they become first-class impact evidence for APIs,
events, schemas, generated clients, and shared libraries.

## Goals

1. Discover cross-repo provider and consumer relationships for HTTP, gRPC,
   events, topics, queues, schemas, generated clients, and shared libraries.
2. Store contract evidence with confidence, source spans, repo generation, and
   ambiguity reasons.
3. Resolve exact and derived provider-consumer links through stable contract
   identities.
4. Feed contract links into dependency neighborhoods, change impact, service
   graphs, and PR automation.
5. Preserve conservative truth semantics for wildcard routes, generated code,
   dynamic clients, and incomplete repo coverage.

## Non-Goals

1. Do not require full semantic compatibility checking in the first release.
2. Do not infer breaking changes from arbitrary source edits without contract
   evidence.
3. Do not treat every string URL, topic name, or library import as an exact
   contract.
4. Do not replace existing repository relationship evidence.
5. Do not require every repository in an organization to be indexed before
   emitting useful derived results.

## Current Flow

Eshu already has relationship evidence for repositories, deployment paths, code
relationships, IaC artifacts, and runtime context. It can explain many
code-to-cloud paths, but cross-repo service contracts are not yet a dedicated
edge family.

That means Eshu can often say which repos deploy or provision together, but it
does not consistently answer:

1. Which services call this HTTP endpoint?
2. Which repos consume this gRPC service or proto package?
3. Which producers and consumers share this event topic?
4. Which generated clients depend on this schema?
5. Which service contracts make a source change risky across repos?

## Problem Statement

Service dependency graphs are usually encoded in contracts, not only in
deployment topology. Without a contract bridge, Eshu risks undercounting blast
radius for API, event, schema, and shared-client changes. It also leaves PR
automation and MCP agents without a reliable way to distinguish internal code
dependencies from cross-service compatibility risk.

The system needs a durable contract identity model that can link provider and
consumer evidence across repositories while staying honest about ambiguous or
partial matches.

## Design

### 1. Contract Identity Model

Introduce a normalized contract identity that can be computed from provider and
consumer evidence.

Initial contract families:

1. `http`: method, normalized path template, host/service hint, route source.
2. `grpc`: package, service, method, proto file, generated-client hint.
3. `event`: broker family, topic or stream, event type, schema hint.
4. `queue`: queue name, direction, environment or namespace hint.
5. `schema`: OpenAPI, protobuf, JSON Schema, Avro, or AsyncAPI artifact.
6. `library`: shared package/module, exported symbol or generated client.

Each identity keeps:

1. normalized key
2. raw observed key
3. provider or consumer role
4. repo id and generation id
5. source path and optional line/range
6. parser or extractor family
7. confidence and truth basis
8. ambiguity or unresolved reason

### 2. Provider And Consumer Extraction

Extraction should start with evidence Eshu can model conservatively:

1. HTTP server routes from frameworks, OpenAPI specs, ingress/service routing,
   and API gateway config.
2. HTTP clients from typed clients, generated clients, OpenAPI references, and
   explicit route constants where reliable.
3. gRPC providers and consumers from proto files, service implementations,
   generated clients, and client stubs.
4. Event and queue producers/consumers from framework adapters, IaC bindings,
   topic config, and schema references.
5. Shared-library providers and consumers from package manifests, module import
   graphs, generated clients, and code ownership metadata.

String-only heuristics may produce derived candidates, but exact contract links
require a stable identity and enough source evidence to explain the match.

### 3. Contract Bridge Resolution

The reducer resolves provider-consumer links by contract family:

1. exact key match when both sides normalize to the same identity
2. route-template match for HTTP when path variables normalize safely
3. wildcard or prefix match only as derived evidence
4. schema artifact match when consumers reference the same OpenAPI/proto/schema
   source
5. generated-client match when the client package can be traced back to a
   provider contract artifact

Resolution output should distinguish:

1. `CONTRACT_PROVIDES`
2. `CONTRACT_CONSUMES`
3. `CONTRACT_MATCHES`
4. `CONTRACT_DERIVED_MATCH`
5. `CONTRACT_UNRESOLVED`

The exact edge names can be refined during implementation, but the response
must preserve whether the match is exact, derived, ambiguous, or unresolved.

Resolution must also preserve bridge freshness. Each resolved or unresolved
answer should know the indexed repo generation, indexed commit when available,
missing repositories, stale repositories, and bridge version that produced the
match. A contract edge from stale or incomplete coverage can still be useful,
but it must be labeled so query consumers do not treat it as complete impact
proof.

Operator-declared manifest links and service boundary assignments should be
modeled before derived matching. When both declared and derived links point to
the same provider-consumer pair, the declared link wins and the derived evidence
is retained as supporting evidence rather than producing a duplicate edge.

### 4. Service Dependency Graph

Resolved contract matches enrich Eshu's service graph:

```text
consumer repo or service
  -> contract consumer evidence
  -> resolved contract identity
  -> contract provider evidence
  -> provider repo, service, workload, environment, and deploy roots
```

This graph should feed:

1. dependency neighborhoods
2. change-impact responses
3. service story and service context
4. PR automation
5. MCP answers about downstream consumers
6. UI `Depended On By`, `Paths`, and `Blast Radius` panes

### 5. Contract-Aware Change Impact

When a changed file touches a provider route, proto service, schema artifact,
event producer, generated client, or shared package export, Eshu should attach
contract impact to the change summary.

The first version should report:

1. directly matched consumers
2. affected repos and services
3. linked workloads and environments when known
4. match exactness and ambiguity
5. missing indexed repositories that limit confidence
6. recommended next checks, such as contract tests or schema compatibility
   validation

The system should not claim a breaking change unless compatibility analysis has
actually run. It should say "this contract has consumers" before it says "this
change breaks consumers."

Cross-repo impact fanout should be bounded. The first implementation should
support a conservative cross depth of one, apply per-repo impact timeouts, honor
request cancellation, and return structured partial results for timeout, stale
bridge state, unsupported depth, missing repo coverage, and high fanout.

### 6. Telemetry

Add telemetry for:

1. extracted provider and consumer evidence by family
2. resolved exact, derived, ambiguous, and unresolved matches
3. contract reducer duration
4. high-fanout contract identities
5. contract-impact query duration and result counts

Avoid labels containing routes, topics, schema names, package names, or repo
paths. Put those in spans, structured logs, and response evidence.

## Proposed Components

1. `go/internal/contracts`: contract identity types, normalization, and matching
   helpers.
2. `go/internal/parser` and `go/internal/relationships`: provider and consumer
   evidence extraction.
3. `go/internal/facts`: durable contract evidence fact envelopes.
4. `go/internal/reducer`: provider-consumer resolution and graph projection.
5. `go/internal/query`: contract-aware dependency and impact readers.
6. `go/internal/mcp`: contract-aware impact and dependency answers.
7. Docs: contract model, truth labels, API examples, and test fixture guidance.

## Error Handling

1. Multiple providers for one identity return all candidates with ambiguity
   reasons.
2. Consumer evidence without an indexed provider becomes `CONTRACT_UNRESOLVED`,
   not a failed query.
3. Wildcard and prefix matches remain derived unless framework or schema
   semantics prove exactness.
4. Generated code must preserve the source schema or generator provenance when
   possible.
5. Missing repo coverage is reported as partial coverage in impact responses.
6. Stale repo snapshots, dangling manifest links, unsupported cross-depth
   requests, and per-repo impact timeouts are reported as structured partial
   states rather than generic failures.

## Testing Strategy

1. Unit tests for contract identity normalization per family.
2. Parser/extractor tests for provider and consumer evidence.
3. Reducer tests for exact, derived, ambiguous, and unresolved matches.
4. Query tests proving contract edges appear in dependency neighborhoods.
5. Diff-impact tests for changed routes, proto files, schema files, topics, and
   generated clients.
6. Fixture ecosystems with at least two repos and one contract family per phase.
7. Negative tests proving string-only or wildcard evidence does not become exact
   without supporting semantics.
8. Concurrency tests for partitioned reducer claims, deterministic write order,
   retry idempotency, stale-generation skip behavior, and no global resolver
   lock.
9. Telemetry tests for extraction coverage, missing repos, stale generations,
   claim lag, fanout, ambiguity, timeout, and partial cross-impact.

## Rollout

### Phase 1: HTTP And OpenAPI Contracts

Model server routes, OpenAPI artifacts, generated clients, and explicit typed
HTTP consumers.

### Phase 2: gRPC And Protobuf Contracts

Model proto services, service implementations, generated clients, and package
or method consumers.

### Phase 3: Events, Topics, Queues, And Schemas

Model producer and consumer evidence for event streams, queues, AsyncAPI, JSON
Schema, Avro, and related IaC bindings.

### Phase 4: Shared Libraries And Generated Clients

Model package exports, shared clients, generated SDKs, and cross-repo library
consumers where source provenance is available.

### Phase 5: Product Integration

Feed contract edges into dependency neighborhoods, change-impact summaries, PR
automation, MCP, CLI, and UI blast-radius panes.

## Open Decisions

1. Whether contract evidence should be its own fact family first or a typed
   specialization of existing relationship evidence facts.
2. Whether exact HTTP matching requires framework route extraction on both sides
   or can accept OpenAPI provider plus generated-client consumer proof.
3. Whether schema compatibility checks should run during ingestion, reducer
   resolution, or query-time impact analysis.
