# Design Gate: Correlation Admission Decision Payload

**Status:** READY FOR IMPLEMENTATION SLICES - design gate for #2693.
**Parent:** #2692.
**Implementation children:** #2744 (schema/store), #2746 (reducer mappings),
#2694 (API/MCP reads), #2695 (golden audit suite), #2747 (docs/workflow).

## Why

Eshu already has reducer-owned correlation decisions, but each domain uses its
own local vocabulary. That makes accurate product explanations harder than the
underlying truth machinery deserves: an operator can see a canonical graph edge
or a domain-specific fact, but cannot ask one uniform question across domains:
why did this edge exist, why did a candidate stay provenance-only, what evidence
was missing, and which follow-up read proves the decision?

This gate standardizes the decision payload vocabulary before implementation.
It does not add schema, code, graph writes, API routes, MCP tools, runtime
knobs, or chart values. Implementation must land in the child issues listed
above.

## Flow And Ownership

Admission remains reducer-owned. Query and MCP surfaces explain decisions; they
do not create new canonical truth.

```text
source facts
  -> reducer candidate
  -> admission decision payload
  -> optional canonical graph or reducer fact write
  -> bounded API/MCP explanation read
  -> golden audit comparing fixture intent, reducer truth, graph truth, and query truth
```

Package ownership:

| Package | Owns |
| --- | --- |
| `go/internal/reducer` | Candidate classification, canonical-write posture, stable decision identity, replay/idempotency behavior. |
| `go/internal/storage/postgres` | Durable decision rows, evidence rows, indexes, bounded list/detail reads. |
| `go/internal/query` | `EvidenceHandler` HTTP read handlers, truth envelope, auth/scoped-token filtering, OpenAPI. |
| `go/internal/mcp` | Thin dispatch to the HTTP handlers with the canonical envelope. |
| `tests/fixtures/product_truth` | Cross-domain fixture intent and expected audit outputs. |
| `docs/public/reference` | Operator-facing state meanings and workflow after implementation. |

Canonical graph edges remain owned by the existing graph materializers and
writers. Rejected, ambiguous, stale, missing-evidence, permission-hidden,
unsupported, and unsafe decisions must never be promoted into graph edges by
the explanation layer.

## State Vocabulary

The shared `state` field is a closed enum:

| State | Meaning | Canonical write posture |
| --- | --- | --- |
| `admitted` | Evidence satisfies the domain's canonical truth rule. | Eligible only when `canonical_write.eligible=true` and the domain already owns that graph or fact write. |
| `rejected` | Input is invalid, too weak, out of scope, policy-denied, or otherwise intentionally refused. | Never writes canonical graph truth. |
| `ambiguous` | More than one candidate can satisfy the selector or evidence is tied. | Never chooses a winner by order. |
| `stale` | Evidence matched only superseded, tombstoned, or older-than-window state. | Retracts or avoids current truth unless the domain has an explicit stale read model. |
| `missing_evidence` | A required source, anchor, endpoint, or corroborating fact is absent. | No canonical write. |
| `permission_hidden` | Evidence may exist, but the source ACL, scoped token, or policy hides it from this read or reducer path. | No canonical write from hidden data. |
| `unsupported` | The provider, source family, relationship type, language, or domain path is outside the modeled contract. | No canonical write. |
| `unsafe` | The payload or relationship token is unsafe to materialize or expose without redaction. | No canonical write; only safe reason classes and handles may be surfaced. |

Domain-specific states such as `exact`, `derived`, `unresolved`,
`partial`, `unavailable`, or `corroborated` are preserved in
`domain_state`. Reducers map them to the shared `state` so the API can filter
uniformly without erasing domain detail.

## Payload Contract

Each persisted admission decision row should carry:

| Field | Contract |
| --- | --- |
| `decision_id` | Stable idempotency key for the domain, anchor, generation, and candidate identity. Must not include mutable source fact ids alone. |
| `domain` | Reducer domain, for example `deployable_unit_correlation` or `package_source_correlation`. |
| `state` | One shared state from the closed vocabulary. |
| `domain_state` | Domain-native state such as `exact`, `derived`, `unresolved`, or `partial`. |
| `anchor` | Safe selector for the subject: repository, workload, service, cloud resource, package, incident, or evidence handle. |
| `candidate` | Safe candidate identity and type. Sensitive or private values must be omitted, redacted, or fingerprinted. |
| `basis` | Bounded reason class and human-readable reason. No raw provider payloads. |
| `confidence` | Score or bucket plus basis such as direct evidence, aggregate evidence, assertion override, or heuristic. |
| `freshness` | State, observed time when safe, generation id, and stale/building cause when proven. |
| `source_handles` | Fact ids, stable fact keys, content handles, or citation handles. Excerpts are out of scope for the core payload. |
| `redaction` | `safe`, `redacted`, `permission_hidden`, or `unsafe`, plus a low-cardinality policy/reason class. |
| `canonical_write` | `eligible`, `written`, `provenance_only`, `skipped_reason`, and graph/fact relationship type when applicable. |
| `next_action` | Recommended bounded follow-up call, audit name, or status route. |

The payload must be JSON-friendly and versioned. The schema/store issue (#2744)
owns the exact DDL and Go structs, but it must keep these fields expressible.

## Compatibility And Migration

The first implementation must coexist with existing reducer fact payloads and
the generic `projection_decisions` / `projection_decision_evidence` tables.
Those surfaces are already consumed by query, MCP, tests, and operator
workflows, so the shared admission payload is additive until a later migration
issue proves read parity and removes the old shape intentionally.

Compatibility requirements:

- Existing reducer fact kinds keep their current payload fields and stable
  fact ids. Shared decisions may reference those facts through `source_handles`
  or carry a normalized copy, but must not rewrite historical fact identity.
- Existing graph writes continue to depend on each domain's current admitted
  row contract. The shared payload cannot become a new implicit write trigger.
- Existing query and MCP routes remain backward compatible. New decision
  explanations are additive fields or sibling routes until clients have a
  documented cutover path.
- Backfill is optional and must be bounded by domain, scope, generation, and
  limit. A backfill that cannot prove source handles or redaction posture must
  emit `missing_evidence` or skip the row rather than inventing provenance.
- Migration proof must compare legacy reducer facts, shared decision rows,
  graph edges, and API/MCP readback for the same fixtures before any old read
  path is deprecated.

Main risks:

| Risk | Mitigation |
| --- | --- |
| Dual-write drift between existing reducer facts and shared decisions. | Idempotent same-transaction writes where possible, focused parity tests, and audit checks that compare both rows. |
| Backfill overstates old evidence because historical payloads lack a field. | Preserve unknowns as `missing_evidence` or omitted optional fields; never synthesize source handles or confidence basis. |
| API clients confuse decision explanations with canonical graph edges. | Keep `decision_explanations` and `canonical_edges` separate in the payload and docs. |
| Permission-hidden or unsafe legacy details leak through normalization. | Normalize only safe reason classes and handles; retain redaction posture as first-class output. |

## Initial Domain Mapping

Implementation starts with three representative domains before expanding:

| Domain | Current local states | Shared mapping |
| --- | --- | --- |
| Deployable-unit and service correlation | Existing payloads include `admission_state`, `confidence`, `evidence_count`, `evidence_kinds`, `rule_pack`, `reason`, `resolved_id`, and graph writes only for admitted rows. Service, Kubernetes, CI/CD, and incident repository correlations already use variants such as `exact`, `derived`, `ambiguous`, `unresolved`, `stale` where applicable, and `rejected`. | `admitted` for exact edge-eligible decisions; `ambiguous`, `missing_evidence`, `stale`, or `rejected` for non-promoted candidates. |
| Package and supply-chain correlation | Package source uses `exact`, `derived`, `ambiguous`, `unresolved`, `stale`, and `rejected`. Supply-chain impact adds impact status, confidence, match reason, missing evidence, evidence path, evidence fact ids, suppression, remediation, and provenance selection. | `admitted` only for domain-approved reducer facts; package-source ownership hints stay `provenance_only` even when exact or derived. `unresolved` maps to `missing_evidence`; unknown or partial impact maps through `domain_state` without pretending it is an admitted graph edge. |
| Cloud inventory and runtime drift | Cloud inventory counts `admitted`, `ambiguous`, `unsupported`, and `unresolved`; runtime drift carries orphaned, unmanaged, ambiguous, and unknown findings with management status, confidence, missing evidence, warning flags, and recommended action. | `admitted`, `missing_evidence`, `unsupported`, `unsafe`, `permission_hidden`, or `ambiguous` with provider-specific `domain_state`. |

This mix exercises graph-edge, reducer-fact, and provenance-only decisions.
Every later domain must document its mapping before emitting shared decisions.
Incident repository correlation is a strong fourth candidate because it already
fails closed on ambiguous or weak routing and persists provenance-only outcomes
with reasons, candidates, and evidence ids.

## Query And MCP Read Shape

#2694 owns implementation, but the gate requires these constraints:

- Scope first: repository, service, workload, cloud resource, package,
  incident, or explicit decision id.
- Required `limit` with deterministic ordering and `truncated`.
- Filters for every shared state.
- HTTP returns the normal Eshu envelope with top-level `data`, `truth`, and
  `error`; freshness remains nested under `truth`.
- MCP adds no data access logic; it dispatches to the HTTP handler.
- Read output separates `decision_explanations` from `canonical_edges`.
- Permission-hidden rows expose counts and reason classes only when the caller
  is allowed to know that hidden evidence exists.
- Recommended next calls must be bounded routes, not whole-graph searches.

The safest HTTP owner is `EvidenceHandler`. The first implementation should
prefer either an additive decision-explanation block on existing evidence routes
or a sibling route under `/api/v0/evidence/.../explanation` when the payload is
distinct. The route should reuse the existing envelope negotiation and
`recommended_next_calls` shape. MCP should either extend the matching existing
tool additively or add an `explain_*` read-only tool that maps to the HTTP route
without MCP-side query logic.

## Golden Audit Shape

#2695 owns implementation. The audit suite must fail when:

- fixture intent and reducer decision disagree;
- a rejected or ambiguous candidate becomes a canonical graph edge;
- an admitted graph edge lacks a decision explanation;
- API and MCP readbacks disagree on state, truncation, or source handles;
- replay or duplicate delivery creates duplicate decisions or changes the
  canonical-write posture;
- stale generations can still present as current admitted truth.

Minimum cases:

| Case | Required proof |
| --- | --- |
| Positive | Admitted decision, expected graph/fact truth, API/MCP readback. |
| Negative | Rejected or missing-evidence decision, no canonical edge. |
| Ambiguous | Multiple candidates retained, no order-based winner. |
| Stale | Superseded or tombstoned evidence visible as stale, not current truth. |
| Permission-hidden | Public-safe count/reason behavior with no private payload leak. |
| Replay | Duplicate delivery and stale generation replay converge. |

The audit suite should register a new `tests/fixtures/product_truth` entry with
expected output under `tests/fixtures/product_truth/expected/`. Reuse the
existing golden graph comparison model where it fits, but keep the reusable
cross-domain decision audit code outside parser-only packages so it can compare
fixture intent, reducer decision rows, graph rows, query rows, and explanation
fields.

## Performance, Concurrency, And Observability

This gate is design-only.

No-Regression Evidence: this document adds no runtime code, schema, query,
graph write, worker, lease, queue domain, batcher, API route, MCP tool, CLI
command, Helm value, or Compose service.

No-Observability-Change: this document adds no telemetry. The implementation
children must either reuse named existing reducer/query/Postgres signals or add
bounded counters, spans, logs, or status fields that expose decision counts,
state filters, truncation, stale/building causes, replay convergence, and
failure classes.

Implementation issues that add schema, graph queries, graph writes, queues,
workers, leases, batching, or runtime stages must run the performance evidence
gate and add tracked evidence in the touched package or public docs.

## Implementation Order

1. #2744: add shared schema/store and tests.
2. #2746: map the first reducer domains and prove replay/idempotency.
3. #2694: add bounded HTTP and MCP readbacks with OpenAPI and parity tests.
4. #2695: add cross-domain golden audit fixtures and CLI or test entrypoint.
5. #2747: publish operator docs after the behavior exists.

Do not close #2692 until all five child issues are closed and a final live or
fixture-backed proof shows reducer decisions, graph truth, API truth, and MCP
truth agree.
