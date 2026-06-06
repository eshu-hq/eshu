# Documentation Updater Actuator Contract

This contract defines the boundary between Eshu's read-only documentation
truth and an external write-capable documentation updater.

Eshu supplies findings, collected documentation facts, evidence packets,
freshness checks, truth labels, and read-permission decisions. The updater owns
drafting, diffing, review, destination credentials, publishing, and publication
audit logs.

## Boundary

Eshu must not store writer-provider API keys, call LLM providers for updater
write workflows, publish documentation, mutate destination systems, or treat
generated prose as more authoritative than source evidence.

The updater must not draft from private graph queries or stale local context
when an Eshu evidence packet exists. It should save the exact packet body and
version used for a run, then check packet freshness before publish.

## Verified Read Surface

The HTTP routes are mounted by `go/internal/query/documentation.go`. MCP tools
route to the same handlers through `go/internal/mcp/tools_documentation.go` and
`go/internal/mcp/dispatch_documentation.go`.

| Purpose | HTTP route | MCP tool |
| --- | --- | --- |
| List visible documentation findings | `GET /api/v0/documentation/findings` | `list_documentation_findings` |
| List collected documentation facts | `GET /api/v0/documentation/facts` | `list_documentation_facts` |
| Read one evidence packet | `GET /api/v0/documentation/findings/{finding_id}/evidence-packet` | `get_documentation_evidence_packet` |
| Check saved packet freshness | `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` | `check_documentation_evidence_packet_freshness` |

The durable documentation read model must be available. When it is not,
handlers return `documentation_read_model_unavailable`. Unsupported profiles
return `unsupported_capability`.

## Request Constraints

`list_documentation_findings` accepts these HTTP query filters:
`scope_id`, `generation_id`, `repo`, `finding_type`, `source_id`,
`document_id`, `status`, `truth_level`, `freshness_state`, `updated_since`,
`limit`, and `cursor`.

`list_documentation_facts` accepts `fact_kind`, `scope_id`, `generation_id`,
`source_id`, `document_id`, `section_id`, `q`, `updated_since`, `limit`, and
`cursor`. `q` searches source display names, document titles, section headings,
section content, and documentation link `target_uri` values within the requested
scope. `fact_kind` may use the short forms `source`, `document`, `section`,
`link`, `entity_mention`, and `claim_candidate`.

Both list routes use RFC3339 timestamps for `updated_since`; `limit` is bounded
from 1 through 200; `cursor` is a non-negative offset returned as
`next_cursor`. Except for `fact_kind=source`, documentation fact listing
requires at least one scope anchor: `scope_id`, `source_id`, `document_id`, or
`section_id`.

No-Regression Evidence: `go test ./internal/query -run 'TestContentReaderDocumentationFacts(SearchesLinkTargetURI|ReturnsEmptyForNoMatch|FiltersAndPaginates)|TestDocumentationHandlerRequiresFactScopeOrAnchor|TestBuildDocumentationFactsSQLIsScopedAndBounded|TestOpenAPISpecIncludesDocumentationFacts' -count=1` and `go test ./internal/mcp -run 'TestListDocumentationFacts(SchemaIncludesBoundedFilters|RouteIncludesScopeAndSearchFilters)' -count=1` cover link target URI matches, non-link text matches, no-match responses, scope-required behavior, SQL bounds, OpenAPI, and MCP routing parity.

No-Observability-Change: link target URI matching stays inside the existing
bounded `documentationFacts` Postgres read and reuses the
`query.documentation_facts` handler span, `db.operation=list_documentation_facts`
Postgres instrumentation, and HTTP/MCP envelope paths. It adds no graph write,
queue, worker, metric instrument, metric label, or runtime deployment knob.

## Evidence Packet Contract

An evidence packet response must include:

- `packet_id`
- `packet_version`
- `generated_at`
- `finding`
- `document`
- `section`
- `bounded_excerpt`
- `linked_entities`
- `current_truth`
- `evidence_refs`
- `truth`
- `permissions`
- `states`

Eshu denies packet evidence when `permissions.viewer_can_read_source` is false,
when `permissions.source_acl_evaluated` is explicitly false, or when
`states.permission_decision` is denied.

Freshness checks compare a saved `packet_version` to the latest packet version
for `packet_id` and return `freshness_state` plus `latest_packet_version`.

## Updater Flow

Use this minimum flow for write automation:

1. List or receive an Eshu finding.
2. Fetch the Eshu evidence packet.
3. Save an immutable updater snapshot of the packet body, packet hash, Eshu
   base URL, finding ID, packet version, updater run ID, writer mode, target,
   actor, and timestamp.
4. Build a deterministic patch plan.
5. Draft within the updater's own provider and policy boundary.
6. Run deterministic verification and any configured semantic verification.
7. Render a diff and apply review or publish policy.
8. Re-check packet freshness immediately before publishing.

High-risk modes such as decision records, compliance pages, runbooks, and
finance prose should remain review-required unless the updater only modifies an
explicitly managed generated section.

## Non-Goals

This contract does not define prompt text, provider APIs, destination publisher
APIs, UI screens, approval UX, updater storage schema, or legal retention
policy beyond immutable packet snapshots.
