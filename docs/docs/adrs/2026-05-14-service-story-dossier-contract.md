# ADR: Service Story Dossier Contract

**Date:** 2026-05-14
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

---

## Context

Issue #280 exposed a product-level MCP gap: `get_service_context`,
`trace_deployment_chain`, `get_relationship_evidence`, and content tools already
held the evidence needed to answer normal service questions, but
`get_service_story` returned a skinny narrative and, over MCP, could appear as a
canonical envelope with `data: null`.

That forced harnesses and Console to know Eshu's internal tool composition:
resolve the service, call context, call deployment trace, dereference evidence,
search content, then stitch a service answer. The older
`2026-04-18-query-time-service-enrichment-gap` ADR called this out as remaining
service-query parity work: service story responses were still narrative-first
rather than complete deployment-mapping contracts.

## Decision

`get_service_story` is the primary one-call service dossier path.

The HTTP route `GET /api/v0/services/{service_name}/story` and the MCP tool
`get_service_story` share the same response model. `get_service_context`,
`trace_deployment_chain`, content reads, and `get_relationship_evidence` become
drill-downs for exact evidence, not required composition for the normal service
answer.

The service story response must include:

- `service_identity` with service/workload id, repo id/name, kind, query basis,
  materialization status, and limitations when known
- `api_surface` with endpoint/method/spec counts and bounded endpoint handles
- `deployment_lanes` that separate GitOps/Kubernetes evidence from
  Terraform/ECS provisioning evidence
- `upstream_dependencies` with typed relationships, confidence, rationale, and
  `resolved_id` when the edge is durable
- `downstream_consumers` with graph dependents separated from content/reference
  consumers
- `evidence_graph` with stable node ids, node categories, relationship edges,
  confidence, evidence counts, and dereferenceable `resolved_id` handles
- `result_limits` with deterministic ordering, limit, truncation, and drill-down
  metadata

MCP keeps routing through the HTTP handler. The query layer owns the contract so
HTTP, MCP, and Console cannot drift.

## Execution Plan

This work was split across independent agent tracks:

- Query/MCP flow trace: map MCP dispatch, HTTP handlers, envelope behavior, and
  the existing service context and deployment trace helpers.
- Documentation and ADR trace: map the prompt/docs language that still taught
  callers to compose multiple tools and define the new ADR contract.
- Main implementation: add failing query/MCP regression tests, implement the
  dossier response at the query layer, update OpenAPI and MCP tool wording, then
  run focused and package verification.

## Rejected Options

### Keep Multi-Call Composition

Rejected. It preserves the current harness problem and makes Console duplicate
query-layer logic. It also keeps service answers dependent on callers knowing
which internal route owns which part of the evidence.

### Add A Separate `get_service_dossier` Primary Tool

Rejected for now. Issue #280 requires `get_service_story` to stop returning
null/skinny data when service context can resolve the service. Adding a second
primary tool would increase MCP tool choice without fixing the existing prompt
contract.

## Bounds And Observability

The dossier is built from the already-scoped service workload context and the
existing service enrichment stages. It does not add a new graph traversal path in
MCP. Large sections expose deterministic ordering, count fields, a shared limit,
truncation metadata, and drill-down handles.

No-Regression Evidence: focused Go tests exercise the dossier shape, empty
section preservation, envelope-backed HTTP response, MCP envelope dispatch, and
OpenAPI service story schema.

No-Observability-Change: the existing service query stage logs emitted by
`startServiceQueryStage` still expose lookup, graph API surface, deployment
evidence, consumer enrichment, provisioning chains, documentation overview, and
overview assembly for `operation=service_story`.

## Consequences

Service explainer prompts now start with one MCP call. Follow-up calls are
evidence drill-downs, not required assembly. Console can consume the same dossier
model without inferring deployment lanes, relationship handles, or consumer
families from lower-level payloads.
