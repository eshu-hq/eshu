# ADR: Local Code Intelligence Host, Authoritative Graph Mode, And Backend Capability Ports

**Date:** 2026-04-20
**Status:** Accepted with follow-up
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `docs/docs/why-eshu.md`
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/deployment/docker-compose.md`
- `docs/docs/reference/local-testing.md`
- `docs/docs/reference/capability-conformance-spec.md`
- `docs/docs/reference/truth-label-protocol.md`
- `docs/docs/reference/local-data-root-spec.md`
- `docs/docs/reference/dead-code-reachability-spec.md`
- `docs/docs/reference/fact-schema-versioning.md`
- `docs/docs/reference/plugin-trust-model.md`
- `docs/docs/reference/local-performance-envelope.md`
- `docs/docs/reference/local-host-lifecycle.md`

---

## Status Review (2026-05-03)

**Current disposition:** Accepted with follow-up; local backend shipped, backend
promotion still gated.

The local-host and local-authoritative split is implemented in the CLI path:
the repo has local host supervision, local graph lifecycle, owner records,
progress reporting, NornicDB installation, query profile contracts, and
unsupported-capability tests.

Current runtime docs and Compose defaults now use NornicDB as the default graph
backend. That default switch does not close the backend ADR: Eshu tracks latest
NornicDB `main` through explicit-source installs for now, while install trust,
broader host coverage, backend conformance, and profile-matrix gates still
live in the implementation plan and NornicDB ADR.

**Remaining work:** finish those NornicDB promotion gates, complete Neo4j parity
research before deciding its final support posture, and keep plugin chunks as
separate work.

## Context

Eshu's product promise is larger than "graph database plus API."

It is supposed to help engineers and AI assistants:

- find definitions by exact and fuzzy name
- search code, comments, and docstrings
- inspect methods, decorators, argument names, imports, and inheritance
- trace direct and transitive callers and callees
- explain call chains and blast radius
- find dead code
- surface complexity hotspots
- connect code to infrastructure and deployment topology

Today, the deployed platform has a clear runtime contract:

- `ingester` owns collection, parsing, and fact emission
- `reducer` owns queued reduction and authoritative graph convergence
- `api` and `mcp` are read surfaces
- the configured graph backend is the authoritative graph store; NornicDB is
  now the default backend and Neo4j remains the official alternative
- Postgres is the durable control plane, fact store, queue, content store, and
  status store

That production split is a strength. It gives us scale boundaries, durable
coordination, and an operator model that fits Docker Compose, Helm, and
Kubernetes.

At the same time, developers want a radically simpler local experience:

- install one binary
- point it at a repo or monofolder
- run a local stdio MCP
- ask code-intelligence questions without standing up the full stack

The earlier "desktop mode" exploration assumed the answer was to make an
embedded graph backend a co-equal local replacement for Neo4j. That path
looked attractive at first, but it creates the wrong maintenance boundary:

- it turns "local mode" into a second graph truth path
- it forces every new graph-backed query feature to be implemented twice
- it blurs the difference between lightweight local answers and authoritative
  transitive graph truth
- it introduces local graph concurrency and lifecycle constraints that do not
  match the production multi-runtime write model

The actual design problem is not "which local graph database do we pick?"
The real problem is:

1. how to give developers a first-class local code-intelligence workflow
2. how to preserve one authoritative production graph path
3. how to make future backend swaps possible without infecting the whole codebase

---

## Problem Statement

Eshu needs a local mode that is excellent for coding workflows without creating
an equal-and-opposite second authoritative graph implementation.

The platform must satisfy all of these constraints:

- production behavior must remain correct, scalable, and operationally familiar
- local CLI and stdio MCP must work well for single-repo and monofolder usage
- code lookup and structural understanding must be first-class locally
- transitive execution tracing, dead-code truth, and change-surface analysis
  must remain authoritative rather than approximate
- future graph backends must be pluggable behind explicit capability ports
- collector extensibility must align with OCI packaging rather than custom
  ad hoc wiring

---

## Decision

### 1. Keep One Authoritative Service-Profile Graph Path

The deployed and full-stack local authoritative path remains:

- split runtimes
- Postgres as durable control plane and content/facts truth
- one configured graph backend behind `GraphQuery` and `GraphWrite`

This ADR does **not** create a second co-equal authoritative graph path for
local mode.

Current implementation note: `ESHU_GRAPH_BACKEND=nornicdb` is now the default in
runtime docs and Compose. Neo4j remains available through the explicit
backend path. NornicDB's default-backend status is still governed by
the conditional acceptance and conformance gates in the NornicDB ADR.

Production remains the source of truth for:

- transitive caller and callee analysis
- call-chain path tracing
- dead-code detection that depends on authoritative reachability
- cross-repo and code-plus-infrastructure blast-radius analysis

### 2. Add A First-Class Lightweight Local Host

Eshu should add a lightweight local host for CLI and stdio MCP that is optimized
for developer workflows, not for replacing the full production runtime shape.

The lightweight local host should:

- run from the `eshu` binary
- embed and manage local Postgres automatically
- index a repo or monofolder without Docker by default
- expose the same query model through CLI and stdio MCP
- support high-value local coding workflows immediately

The lightweight local host is intended to excel at:

- exact symbol lookup
- fuzzy code search
- variable lookup
- content search across source, comments, and docstrings
- methods on class
- decorators and argument-name search
- imports and references
- inheritance and implementation discovery where semantic facts suffice
- complexity analysis and top-N hotspot queries

### 3. Distinguish Capability Tiers Explicitly

Eshu should stop pretending all answers come from the same truth source.

Every code query belongs to one of these capability tiers:

#### Local code-intelligence tier

Backed by indexed entities, structured content, semantic facts, and relational
query tables.

Examples:

- definition lookup
- fuzzy search
- list nodes by type
- variable lookup
- content/docstring/comment search
- decorators
- argument names
- methods on class
- imports
- complexity

#### Authoritative graph tier

Backed by reduced, authoritative graph relationships.

Examples:

- direct callers and callees
- transitive callers and callees
- call-chain paths
- dead code
- precise blast-radius analysis

#### Hybrid platform tier

Backed by code, graph, and infrastructure/deployment truth together.

Examples:

- change surface across repos and IaC
- service-to-resource tracing
- dependency explanations that cross code and platform boundaries

### 4. Label Query Truth Explicitly

Eshu should distinguish response truth levels:

- `exact` for authoritative graph or durable semantic truth
- `derived` for deterministic relational/entity-derived answers
- `fallback` for exploratory approximations that should not be treated as full
  transitive or dead-code truth

CLI, MCP, and HTTP surfaces should use the same labeling model.

For unsupported high-authority questions in lightweight local mode, Eshu should
return a structured unsupported-capability error rather than silently degrade a
transitive, path, or dead-code question into an exploratory fallback answer.

### 5. Use Capability Ports, Not Database Brands, As The Core Abstraction

The internal architecture should depend on Eshu capability ports, not directly
on "Neo4j," "Ladybug," or any future backend brand.

The key boundaries should be:

- `FactStore`
- `ContentStore`
- `GraphWrite`
- `GraphQuery`

Graph backends become adapters behind these capabilities.

These capability ports do not exist as named interfaces today. This ADR is
choosing the target architecture; the extraction work is net-new.

The initial read-path extraction is expected to land at the storage seam first:

- `GraphQuery` for graph traversals and point lookups
- `ContentStore` for relational content, entity, and coverage reads

Higher-order capability groupings such as `CodeSearch`, `SymbolGraph`, and
`CallGraph` remain valid product-level concepts, but they should only become
separate internal interfaces if the storage-seam ports prove too coarse in
tests or adapter implementations.

This ADR explicitly rejects introducing an ORM as the central abstraction.
An ORM is the wrong boundary for graph traversal, semantic code queries,
projection flows, and transitive dependency analysis.
This does not prohibit normal SQL helper libraries for relational query
implementation details.

### 5.a Optional Graph Backend Sidecar For `local_authoritative`

The "embedded graph as co-equal local truth path" rejection earlier in
this ADR stands. A **sidecar** graph backend — installed as a separate
binary, owned by the Eshu lightweight host lifecycle, addressable through
a local socket, tracked in `owner.json` — is a distinct design that this
ADR endorses.

Eshu introduces a new profile, `local_authoritative`, that runs the
lightweight local host plus a user-installed graph-backend sidecar. The
sidecar unlocks transitive callers, call-chain paths, dead-code
detection, and the hybrid platform-impact capabilities at laptop scale
without requiring Docker Compose.

NornicDB is the supported default backend for this profile. The remaining
Neo4j support-decision path lives in
`docs/docs/adrs/2026-05-04-neo4j-parity-optimization-plan.md`.

### 6. Treat Collector Extensibility As A Fact-Emission Plugin Seam

Future collector extensibility should happen at the fact-emission boundary.

Collectors should:

- discover or extract evidence
- emit versioned facts
- avoid writing graph truth directly

Collectors should be distributable as OCI-packaged plugins so developers can
add new collector families without patching the core runtime by hand.

Any plugin implementation must include a trust model before activation in the
core runtime, including artifact provenance, signing or allowlisting, and an
explicit compatibility check against supported fact-schema versions.

---

## Performance And Reliability Envelope

The lightweight local host must be falsifiable, not just "convenient."

Initial targets:

- cold local-host startup: under 5 seconds on a typical developer laptop
- warm local-host restart: under 2 seconds
- indexed code-lookup queries: sub-second at p95 on an actively used repo
- memory budget: documented and measured separately for idle host and active
  indexing

If the local host cannot meet those targets, it should still be honest about
state and capability rather than masking the miss behind broader fallbacks.

See `local-performance-envelope.md` for the maintained target envelope.

---

## Local Host Ownership And Data Root

The lightweight local host should treat each workspace data root as
single-owner.

That requires:

- a versioned data-root layout
- an ownership lock protocol for the workspace root
- stale-lock detection on crash or unclean shutdown
- explicit migration or reset behavior when the on-disk schema version changes

Second invocations against the same workspace must either attach safely to the
existing owner or fail fast with an actionable error. They must not race as
independent writers against the same local data root.

---

## Query Parity Rule

Eshu should publish and test a profile-aware capability matrix. Lightweight
local, full local stack, and production do not need identical internals, but
they do need an explicit contract for which queries are exact, derived,
unsupported, or authoritative in each profile.

---

## What This Means For Local And Production

### Lightweight local mode

Target workflow:

```bash
eshu watch .
eshu mcp stdio
eshu search "process_payment"
eshu analyze methods Order
eshu analyze complexity --top 10
```

Primary purpose:

- local coding assistance
- repo or monofolder understanding
- low-friction stdio MCP

Not the place to promise authoritative transitive graph truth unless the
runtime has actually built that truth.

### Full local stack

Target workflow:

```bash
docker compose up --build
eshu analyze callers process_payment
eshu analyze call-chain main process_payment
eshu analyze dead-code
```

Primary purpose:

- authoritative pre-merge validation
- reducer convergence testing
- full graph-backed query parity with production

### Production

Primary purpose:

- authoritative multi-repo platform truth
- scalable runtime isolation
- incident, refactor, and blast-radius analysis

---

## Rejected Alternatives

### Embedded graph backend as a co-equal local truth path

Rejected for now.

Reason:

- doubles query and reducer maintenance burden
- encourages feature drift between local and production
- weakens confidence in graph-backed answers
- does not match the production split-runtime ownership model

### ORM-centric architecture

Rejected.

Reason:

- graph traversal and path semantics are not well represented by an ORM boundary
- Eshu's real abstractions are semantic/query capabilities, not tables and rows

### Hiding capability differences from users

Rejected.

Reason:

- users need to trust whether an answer is exact, derived, or fallback
- dead code and transitive impact answers are worse than useless if presented as
  exact without authoritative backing

---

## Consequences

### Positive

- production remains stable and single-sourced
- local MCP becomes first-class without requiring a full graph clone of prod
- query contracts become clearer and more testable
- future backend evaluation becomes safer through conformance testing
- collector extensibility gains a clean OCI-based distribution story

### Negative

- lightweight local mode will not claim full authoritative graph parity
- some high-value graph questions will still require full local stack or
  deployed environment
- capability labeling adds API and MCP surface work

### Operational guardrails

- no production runtime should depend on local-only storage behavior
- local capability shortcuts must not silently change service-profile truth
- new graph backends must pass conformance before being called supported
- local host ownership, upgrade, and shutdown behavior must be deterministic

---

## Validation Requirements

Before this ADR can be called implemented, Eshu must prove:

1. lightweight local mode supports the core code-intelligence surface in CLI
   and stdio MCP
2. full local stack and production preserve authoritative graph-backed queries
3. truth-level labeling is visible and accurate
4. new capability ports do not regress the current service runtime contract
5. backend conformance tests exist for any backend presented as a supported
   graph adapter

---

## Status Summary

This ADR changes the direction of "desktop mode" from:

- "pick an embedded graph database and make it co-equal to production"

to:

- "build a first-class lightweight local code-intelligence host, keep one
  authoritative production graph path, and use capability ports plus
  conformance testing to enable future backend swaps safely"
