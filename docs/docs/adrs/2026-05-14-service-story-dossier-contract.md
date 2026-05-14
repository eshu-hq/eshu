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

The use-case and starter-prompt docs had a stronger promise than this first
slice implemented. `docs/docs/use-cases.md` tells documentation callers to ask
Eshu to "scan all related repositories, deployment sources, and indexed
documentation" and to prefer `investigate_service` for that workflow.
`docs/docs/guides/starter-prompts.md` repeats that prompt pattern across
onboarding, runbook, and GitOps documentation examples. `docs/docs/reference/http-api.md`
also documented `GET /api/v0/investigations/services/{service_name}` with
coverage fields, but the route and MCP tool did not exist. Leaving that mismatch
would make the docs overpromise and would keep harnesses guessing which repos
were considered.

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
- `investigation` with the same coverage packet exposed by
  `investigate_service`: repositories considered, repositories with evidence,
  evidence families found, coverage summary, investigation findings, and
  recommended next calls

MCP keeps routing through the HTTP handler. The query layer owns the contract so
HTTP, MCP, and Console cannot drift.

Add `investigate_service` as an explicit MCP inspection tool and
`GET /api/v0/investigations/services/{service_name}` as its HTTP backing route.
This route is investigation-first rather than story-first. It does not replace
`get_service_story`; it gives harnesses and Console a bounded way to ask "what
did Eshu consider, what evidence families were found, and what should I call
next if I need proof?"

Coverage remains truthful. The service investigation reports `partial` when it
has evidence but cannot prove exhaustive cross-repository coverage, and
`unknown` when no evidence families are present. It does not report `complete`
unless a future indexed coverage source explicitly proves complete coverage.

## Execution Plan

This work was split across independent agent tracks:

- Query/MCP flow trace: map MCP dispatch, HTTP handlers, envelope behavior, and
  the existing service context and deployment trace helpers.
- Documentation and ADR trace: map the prompt/docs language that still taught
  callers to compose multiple tools and define the new ADR contract.
- Main implementation: add failing query/MCP regression tests, implement the
  dossier response at the query layer, update OpenAPI and MCP tool wording, then
  run focused and package verification.
- Continuation after the use-case doc review: add failing investigation tests,
  implement the documented service investigation route and MCP tool, embed the
  investigation packet into `get_service_story`, update OpenAPI, and revise this
  ADR plus the starter/use-case guidance.

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

### Keep `investigate_service` As Documentation Only

Rejected. The route and tool were already documented as the preferred widening
workflow. Keeping them as aspirational docs would leave the MCP harness with no
bounded coverage packet and would make the starter prompts unreliable.

## Bounds And Observability

The dossier is built from the already-scoped service workload context and the
existing service enrichment stages. It does not add a new graph traversal path in
MCP. Large sections expose deterministic ordering, count fields, a shared limit,
truncation metadata, and drill-down handles.

The investigation route uses the same scoped service lookup and enrichment path
as service story. It packages existing context into coverage and next-call
handles; it does not add unbounded graph traversal or content search.

The query contract must not depend on a cache layer. This PR keeps the new MCP
surface bounded by reusing the existing service-scoped query path, capping raw
context arrays at `serviceStoryItemLimit`, deduplicating deployment evidence
previews by durable relationship handle, and reporting truncation/coverage
metadata instead of returning every preview row.

The current service-story path was audited before merge because MCP does not
have a cache layer. The request is sequential only where a later read depends on
the earlier result: workload lookup must resolve `repo_id` and workload
instances first, consumer evidence needs service evidence hostnames, and
deployment fallback must know whether graph/read-model deployment evidence
already exists. The audit found two unnecessary graph costs and this PR removes
both:

- provisioning repository candidates are now queried once per service
  enrichment and reused for graph dependents, consumer enrichment, and
  provisioning source chains instead of running the same bounded traversal three
  times;
- workload-instance platform evidence is now fetched with one indexed
  `WorkloadInstance.id IN $instance_ids` graph read instead of one Bolt query
  per instance.

The audit also found follow-up query-boundary risks that remain outside this
PR's service-dossier contract: graph API-surface rows are scoped but not
detail-limited at the Cypher boundary, framework-route facts are repo-scoped but
not row-limited in SQL, durable deployment-evidence read-model rows are scoped
but not capped before JSON preview decoding, and consumer evidence can fall back
to cross-repo `LIKE`/`ILIKE` content search when `content_file_references` is
unavailable. Those are tracked by issue #301 and the broader prompt coverage
hardening issue #300 so they can receive focused count/truncation contracts
without silently hiding evidence in this PR.

No-Regression Evidence: focused Go tests exercise the dossier shape, empty
section preservation, envelope-backed HTTP response, MCP envelope dispatch, and
OpenAPI service story schema. Continuation tests cover the embedded
investigation packet, the service investigation HTTP route, the
`investigate_service` MCP dispatch route, tool registration, and OpenAPI
investigation schema. Review-thread tests cover aggregate API count fallbacks,
`spec_paths` fallback behavior, raw payload caps, nested API-surface caps,
relationship-preview deduplication, graph-dependent evidence nodes, and
downstream truncation metadata that matches the independently capped sections.
Focused performance-shape tests prove the removed duplicate work:
`TestEnrichServiceQueryContextQueriesProvisioningCandidatesOnce` fails on the
old three-traversal service enrichment and passes with one candidate query, and
`TestFetchWorkloadPlatformRowsBatchesExactInstanceIDs` proves one indexed
platform lookup for multiple workload instances.

Observability Evidence: the existing service query stage logs emitted by
`startServiceQueryStage` still expose lookup, graph API surface, deployment
evidence, graph provisioning candidates, consumer enrichment, provisioning
chains, documentation overview, and overview assembly for
`operation=service_story` and `operation=service_investigation`. Existing
`neo4j.query` and `postgres.query` spans expose the backing Cypher and SQL
statements, so a slow MCP answer can be split into service lookup, graph read,
Postgres content read, and response assembly time without adding a cache layer.

## Consequences

Service explainer prompts now start with one MCP call. For normal answers, that
call is `get_service_story`; for widening and coverage inspection, it is
`investigate_service`. Follow-up calls are evidence drill-downs, not required
assembly. Console can consume the same dossier and investigation model without
inferring deployment lanes, relationship handles, consumer families, or coverage
state from lower-level payloads.
