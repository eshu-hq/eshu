# Local Lightweight Capability Audit

This page records the **refuse-vs-degrade decision** for every capability the
`local_lightweight` profile does not support, and explains why. It is the audit
deliverable for issue #3027 (epic #3012).

`local_lightweight` runs Eshu against an embedded Postgres **content index**
only — no graph sidecar and no reducer-materialized graph. The machine-readable
source of truth for per-profile support stays
`specs/capability-matrix.v1.yaml` plus `specs/capability-matrix/*.yaml`; this
page does not duplicate that list, it explains the decision behind it.

## Scope of the refusal

| Matrix scope | Capabilities | `local_lightweight` unsupported |
| --- | --- | --- |
| Core matrix (`capability-matrix.v1.yaml`) | 88 | 63 |
| Full matrix (core + 16 fragments) | 114 | 75 |

The "63 of 352 cells" figure in epic #3012 is the core matrix (88 capabilities ×
4 profiles). The fragment files add twelve more graph/reducer-backed refusals.

## The governing rule: refuse, do not silently downgrade

The refusal is a deliberate correctness choice, not a gap. The truth-label
protocol requires it:

> High-authority capabilities such as transitive call graphs, call-chain paths,
> and dead-code cleanup must return `unsupported_capability` when the active
> profile cannot answer them correctly. They must not silently downgrade to
> `fallback`.
>
> — [Truth Label Protocol](truth-label-protocol.md)

A capability may only claim a truth level the underlying data can honestly
support: `exact` from the authoritative graph or durable semantic facts,
`derived` from the deterministic content index or relational state, and
`unsupported` when neither is available (see
[Capability Conformance Spec](capability-conformance-spec.md)). The runtime gate
is `capabilityUnsupported(profile, capability)` in
`go/internal/query/handler.go`, which returns `unsupported_capability` whenever
`maxTruthLevel(capability, profile)` is nil — before any backend is queried.

Degrading a graph capability to a content-index guess would manufacture a
low-authority answer for a question the profile cannot answer correctly, which
the protocol forbids. So the audit's default verdict is **refuse**, and a cell
moves to **degrade** only when the content index already holds the exact data a
bounded, honestly-labelled variant needs.

## Decision by category

Every `local_lightweight`-unsupported capability falls into one of these
categories. All require either the authoritative graph or reducer-materialized
facts that simply do not exist in the content-index-only profile.

| Category | Examples (capability IDs) | Why the content index cannot serve it | Decision |
| --- | --- | --- | --- |
| Call-graph traversal | `call_graph.transitive_callers`, `call_graph.transitive_callees`, `call_graph.call_chain_path`, `call_graph.metrics` | No `CALLS` edges exist outside the graph (see audit below) | Refuse |
| Graph-backed code analysis | `graph_query.read_only_cypher`, `symbol_graph.import_dependencies`, `code_quality.refactoring`, `code_quality.dead_code`, `code_to_cloud.trace_exposure_path` | Need graph traversal or the current graph-backed code-quality contract; parser metadata coverage is not a bounded content-index variant | Refuse |
| Platform / infrastructure graph | `platform_impact.*` (deployment chain, blast radius, entity map, resource-to-code, environment compare, …) | Require an authoritative platform/infrastructure graph | Refuse |
| Reducer-materialized correlations | `ci_cd.run_correlations.*`, `service_catalog.correlations.list`, `kubernetes.correlations.list`, `observability.coverage.correlations.list` | Correlation facts are produced by the reducer, absent in lightweight | Refuse |
| Supply chain & security | `supply_chain.*`, `secrets_iam.*`, `aws_runtime_drift.findings.list`, `iac_management.*`, `iac_inventory.resources.list` | Require reducer-materialized SBOM/IAM/drift/IaC facts | Refuse |
| Durable read models | `documentation_findings.*`, `documentation_facts.list`, `documentation_evidence_packet.*`, `package_registry.*`, `dependencies.list`, `relationship_evidence.drilldown` | Require durable reducer read models / package graph nodes | Refuse |

In every category the missing input is graph topology or reducer output, neither
of which the content index contains. There is no safe bounded heuristic, so the
matrix correctly refuses.

## The three degrade candidates called out in #3027

Issue #3027 asked specifically whether three families could be answered from the
content index with bounded heuristics. Each was checked against the actual
content-index schema.

### Transitive callers / callees (depth ≤ 2) — Refuse

The content index has no direct call edges. When the graph is absent, the
relationship fallback (`buildOutgoingContentRelationships` in
`go/internal/query/content_relationships.go`) returns only infrastructure
references (Terraform, Kubernetes, Dockerfile, ArgoCD, CloudFormation, …) and
JSX component usage — **zero code-call edges**. `CALLS` edges exist only as graph
relationships (`[:CALLS*1..N]` traversal in
`go/internal/query/code_call_chain.go`). With no depth-1 edges
there is nothing to expand to depth 2, so any "transitive callers" answer would
be fabricated. Refuse stands.

> Note: `call_graph.direct_callers` / `call_graph.direct_callees` are already
> marked `derived`-supported on `local_lightweight`. They return an **empty**
> content-fallback result (honestly labelled `content_index`) rather than a
> guess, so no transitive or degree metric can be synthesized from them either.

### Code-quality refactoring candidates (high-arg, long-function) — Refuse

Function **line span** is available in the content index
(`content_entities.start_line` / `end_line`), but the refactoring check also
needs `parameter_count` and `cyclomatic_complexity`. Those values are available
for at least Go parser function rows as entity metadata: the Go parser emits the
fields, `shape.Materialize` copies parser item metadata into
`content.EntityRecord.Metadata`, and the Postgres content writer persists that
map in `content_entities.metadata`. The shipped `code_quality.refactoring`
handler is still a graph-backed contract, however: it reads graph node
properties in `go/internal/query/code_quality.go`, and there is no bounded
content-index variant that reads JSONB metadata, defines language coverage, or
surfaces "metric unavailable" rows for parsers that do not emit the fields. A
"long function" sub-signal could be derived, and some languages can provide the
other metrics from metadata, but the capability as specified cannot yet be
honestly served from the content index alone. Refuse stands; a future bounded
content-index variant would need explicit metadata coverage and missing-metric
semantics before revisiting this.

### Call-graph metrics (in-degree / out-degree) — Refuse

In/out-degree is a count over `CALLS` edges. As established above, those edges
live only in the graph, so degree metrics would all be zero (i.e. wrong) on
`local_lightweight`. Refuse stands.

## Relationship to the default-profile change

Epic #3012 also changed the default `eshu mcp start` owner to
`local_authoritative` (see [Local Host Lifecycle](local-host-lifecycle.md) and
[Local Performance Envelope](local-performance-envelope.md)). That is the
primary fix: a fresh MCP session boots the embedded graph and answers these 63
core-matrix capabilities directly. `local_lightweight` therefore remains a deliberate,
faster Postgres-only opt-in (`--profile local_lightweight`), and its refusals
are correct by design rather than a coverage gap.

## Follow-up

Re-opening any refuse decision requires landing the missing data in the content
index first (for example content-index call edges or a bounded code-quality
metadata read model), with the new bounded variant added to the matrix and
proven by a `go_test` verification entry. None of those projections exist today,
so no matrix change ships with this audit.
