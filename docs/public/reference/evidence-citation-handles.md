# Evidence Citation Handle Contract

Evidence citation handles are stable, bounded pointers that answer-facing
surfaces pass to `POST /api/v0/evidence/citations` or the
`build_evidence_citation_packet` MCP tool. A handle names evidence; the citation
builder hydrates only evidence the current query profile and backing store can
read. Handles never authorize new scope, never call providers, and never carry
raw source payloads by themselves.

The current runtime hydrates `file` and `entity` handles. This contract defines
the backward-compatible expansion path for facts, relationships, documentation
packets, incident slots, and supply-chain findings so `AnswerPacket` and
`VisualizationPacket` can carry the same handle shape without inventing source
data.

## Compatibility Rules

- Existing `file` handles keep the wire shape `{kind, repo_id, relative_path,
  start_line, end_line, evidence_family, reason}`.
- Existing `entity` handles keep the wire shape `{kind, entity_id, repo_id,
  evidence_family, reason}`. `repo_id` remains optional context for entity
  handles and is not the lookup key.
- New fields are additive and optional. A client that only understands file and
  entity handles can ignore unknown fields and still hydrate the legacy handles.
- The citation route remains bounded: at most 500 requested handles and at most
  50 hydrated citations per packet unless the code-level limit changes with
  performance evidence.
- Ordering is deterministic: normalize, deduplicate by the full handle identity,
  retain caller order for the accepted page, and report truncation before
  validating handles beyond the page probe.

## Handle Kinds

| Kind | Required fields | Source store | Safe excerpt fields | Redaction and permission rules |
| --- | --- | --- | --- | --- |
| `file` | `repo_id`, `relative_path`; optional line range | `content_files` through the `ContentStore` | Bounded source excerpt, language, artifact type, content hash, commit SHA | Preserve file-content authorization. Respect requested line bounds and cap excerpts. |
| `entity` | `entity_id`; optional `repo_id` | `content_entities` through the `ContentStore` | Entity signature/body excerpt, entity type, name, language, source path | Preserve entity-content authorization. Do not synthesize line ranges when none exist. |
| `fact` | `fact_id`; optional `fact_kind`, `scope_id` | `fact_records` through a generic fact-citation read port | Fact kind, scope, generation, confidence, observed time, source URI host/path when allowed, and a reducer-approved summary | Never expose raw fact payloads by default. Hide denied documentation facts and secret-like values. |
| `relationship` | `resolved_id` | `resolved_relationships` / relationship evidence read model | Source/target labels, relationship type, confidence, rationale, evidence preview, generation metadata | Use the existing relationship drilldown authorization. Missing rows are missing handles, not graph re-queries. |
| `documentation_finding` | `finding_id` | Documentation finding fact read model | Finding type, status, truth level, freshness, source/document IDs, evidence packet URL | Reuse documentation permission predicates. Denied findings are reported as missing or permission-hidden without counts or payload. |
| `documentation_packet` | `packet_id`; optional `packet_version` | Documentation evidence packet read model | Packet ID/version, finding ID, freshness state, bounded claim/evidence summary | Reuse packet freshness and permission checks. Do not embed document body or proposed edits. |
| `incident_slot` | `provider`, `provider_incident_id`, `slot`; optional `scope_id`, `service_id` | Incident context read model | Slot, truth label, explanation, bounded value keys, evidence refs | Provider APIs are not called. Ambiguous incidents require scope. Raw incident payloads, assignees beyond sanitized references, and provider-only private fields stay out. |
| `supply_chain_finding` | `finding_id`; optional `cve_id`, `advisory_id`, `package_id`, `repository_id`, `subject_digest` | Supply-chain impact finding read model | Finding ID, advisory/CVE, package, impact status, priority, reachability state, evidence fact IDs, missing evidence | Reuse suppression visibility and scoped filters. Hidden suppressed rows remain hidden unless the caller explicitly requested the same visibility on the source read. |

`fact_id`, `resolved_id`, `finding_id`, `packet_id`, `provider`,
`provider_incident_id`, `slot`, `scope_id`, `service_id`, `cve_id`,
`advisory_id`, `package_id`, `repository_id`, and `subject_digest` are reserved
top-level handle fields for these expansions.

## Missing And Unsupported Handles

The citation packet must distinguish structural request errors from evidence
that cannot be hydrated:

- A known kind with missing required fields is a bad request.
- A known kind whose source row is unavailable, denied, stale beyond the source
  read model, or outside the configured runtime profile is returned in
  `missing_handles`.
- An unknown kind is returned in `missing_handles` with an unsupported reason
  once expanded-handle normalization is implemented. Legacy deployments may
  still reject unknown kinds before that migration.
- Permission-hidden evidence is not converted into an empty citation. The
  missing handle should retain the original handle identity and carry a stable
  reason when the wire type supports it.

Recommended next calls must stay bounded and actionable. For example,
relationship handles point to `GET /api/v0/evidence/relationships/{resolved_id}`;
documentation packet handles point to the packet freshness route; incident slot
handles point to `get_incident_context`; supply-chain finding handles point to
`GET /api/v0/supply-chain/impact/findings` with the same stable anchor.

## Answer And Visualization Rules

`AnswerPacket` and `VisualizationPacket` may carry expanded handles only when
the source response already carried the required fields. Builders must not query
stores, invent identifiers, infer missing source rows, or convert a free-text
label into a handle. If the source response only has a display label, the packet
omits the handle and records a limitation or missing-evidence entry.

Visualization nodes and edges keep the same invariant: an `evidence_handle`
points back to the citation handle a client would send for hydration. The
visualization builder never copies citation excerpts into nodes and never widens
the source response scope.

## Implementation Gates

Runtime support for a new handle kind must include:

- Unit tests for normalization, deduplication, deterministic ordering,
  truncation, missing handles, unsupported kinds, and required-field validation.
- Store tests proving bounded hydration from the named source store, including
  denied/permission-hidden rows and redacted excerpts.
- HTTP and MCP parity tests proving the same handle set resolves to the same
  citation, missing-handle, coverage, and recommended-next-call shape.
- `AnswerPacket` and `VisualizationPacket` tests proving expanded handles pass
  through only from source response data.
- Observability evidence showing the existing citation packet span and
  Postgres-query spans expose the source store and failure mode operators need
  for diagnosis.
- Performance evidence or an explicit no-regression measurement for any new
  batched read, including the 500-handle input cap and 50-citation output cap.
