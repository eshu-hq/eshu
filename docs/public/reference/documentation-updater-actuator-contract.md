# Documentation Updater Actuator Contract

This contract defines how a write-capable documentation updater may use Eshu.
Eshu provides read-only documentation findings and evidence packets. The
external updater owns drafting, review, diffing, and publishing outside Eshu.

For MCP tool names, see [MCP Reference](mcp-reference.md). For the
deterministic verifier package, see `go/internal/doctruth/README.md`.

## Boundary

Eshu owns read-only documentation truth:

- documentation source, document, section, link, mention, and claim-candidate
  facts
- deterministic documentation claim verification
- durable documentation findings
- evidence packet assembly
- truth labels, freshness, ambiguity, and unsupported states
- permission-aware evidence denial

The external updater owns write behavior:

- LLM provider adapters and customer-managed API keys
- prompt templates, writer modes, and style profiles
- deterministic patch planning
- bounded LLM drafting
- deterministic and optional semantic verification
- diff rendering
- approval workflow
- destination publisher adapters
- publication audit logs

Eshu core must not store provider API keys, call LLM providers for write
workflows, publish documentation, mutate destination systems, or treat prose as
more authoritative than source evidence.

## Current Read APIs

The documentation read routes are mounted by `go/internal/query/documentation.go`.
MCP exposes the same routes through `go/internal/mcp/tools_documentation.go`.

| Purpose | HTTP route | MCP tool |
| --- | --- | --- |
| List visible documentation findings | `GET /api/v0/documentation/findings` | `list_documentation_findings` |
| List collected documentation facts | `GET /api/v0/documentation/facts` | `list_documentation_facts` |
| Read one evidence packet | `GET /api/v0/documentation/findings/{finding_id}/evidence-packet` | `get_documentation_evidence_packet` |
| Check saved packet freshness | `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` | `check_documentation_evidence_packet_freshness` |

These routes return read-only evidence. They do not draft text or write to
destination systems.

## List Findings

`GET /api/v0/documentation/findings`

Use this route to list findings safe for the caller to inspect. The read model
filters out findings when source visibility is denied, unknown, or not
evaluated.

Supported filters:

- `scope_id`
- `generation_id`
- `repo`
- `finding_type`
- `source_id`
- `document_id`
- `status`
- `truth_level`
- `freshness_state`
- `updated_since` as RFC3339
- `limit` from `1` through `200`
- `cursor` as a non-negative offset returned by `next_cursor`

The response returns `findings` and `next_cursor`. Each finding includes
identity, type, status, truth/freshness state, source/document/section identity,
summary, and evidence-packet URL.

## List Documentation Facts

`GET /api/v0/documentation/facts`

Use this route for audit and inspection of collected documentation facts. The
updater's immutable write-planning source should be the evidence packet when one
exists.

Supported filters:

- `fact_kind`: `source`, `document`, `section`, `link`, `entity_mention`, or
  `claim_candidate`
- `scope_id`
- `generation_id`
- `source_id`
- `document_id`
- `section_id`
- `q` for case-insensitive text search over source display name, document
  title, section heading, and section content
- `updated_since` as RFC3339
- `limit` from `1` through `200`
- `cursor` as a non-negative offset returned by `next_cursor`

Except for `fact_kind=source`, the request must include at least one of
`scope_id`, `source_id`, `document_id`, or `section_id`.

## Get Evidence Packet

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet`

Use this route before planning any updater diff. The updater must not infer
write context from arbitrary private graph queries.

Required packet fields:

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

Important state fields:

| Field | Meaning |
| --- | --- |
| `finding.status` | Finding state such as `conflict`, `stale`, `ambiguous`, or `unsupported`. |
| `states.finding_state` | Current state repeated for updater policy checks. |
| `states.freshness_state` | Packet freshness state. |
| `states.unsupported_reason` | Why the packet is unsupported, when applicable. |
| `states.stale_reason` | Why the packet is stale, when applicable. |
| `states.permission_decision` | Permission decision, such as `allowed` or `denied`. |
| `permissions.viewer_can_read_source` | Must be `true` before Eshu returns packet evidence. |
| `permissions.source_acl_evaluated` | If explicitly `false`, findings are not listed and packets are denied. |

Save the packet before drafting and check freshness before publishing.

## Check Packet Freshness

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness?packet_version={saved_packet_version}`

Use this route immediately before publishing a diff. If the saved version
differs from the latest version, Eshu returns `freshness_state: "stale"` and
the latest version. The route still returns `200`; the updater enforces policy.

```json
{
  "packet_id": "doc-packet:service-deployment:1",
  "packet_version": "1",
  "freshness_state": "stale",
  "latest_packet_version": "2"
}
```

## Error States

| HTTP status | Error code | Updater behavior |
| --- | --- | --- |
| `400` | `invalid_argument` | Fix invalid timestamp, pagination, fact kind, or missing scope before retrying. |
| `403` | `permission_denied` | Do not draft. |
| `404` | `not_found` | Drop or refresh the item. |
| `500` | `internal_error` | Retry according to updater policy. |
| `501` | `documentation_read_model_unavailable` | Disable drafting and alert the operator. |
| `501` | `unsupported_capability` | Use a runtime profile that supports durable documentation routes. |

Non-envelope error bodies include `error_code`, `message`, `correlation_id`,
and `capability` when the error is capability-specific. If the caller sends
`Accept: application/eshu.envelope+json`, Eshu returns the same error in the
canonical response envelope.

## Immutable Evidence Snapshots

Before drafting, the updater must save the exact packet it used. The snapshot
must include the raw packet body, packet hash, Eshu base URL, finding and packet
versions, updater run ID, writer mode ID/version, destination target ID, actor
or service identity, and created timestamp.

Do not rewrite a saved packet after drafting starts. If Eshu returns a newer
packet version, create a new updater run.

## Permission Expectations

Eshu decides whether the caller can read Eshu evidence. The updater decides
whether it can write to the destination.

Eshu must enforce read permissions, deny evidence when visibility is unknown or
ACLs were not evaluated, include permission state in the packet, and avoid
storing destination write tokens.

The updater must verify destination write permission, keep destination
credentials outside Eshu, honor review-required policy, and record who approved
or published a change.

Confluence is collected through read-only credentials. Source and document ACL
summaries are evidence for security review, not write permission grants.

## Minimum Updater Flow

```text
Eshu finding
  -> Eshu evidence packet
  -> immutable updater snapshot
  -> deterministic patch plan
  -> writer mode and style profile
  -> bounded LLM draft
  -> deterministic verifier
  -> optional semantic verifier
  -> rendered diff
  -> approval or publish policy
```

The LLM must not decide whether documentation is stale. Writer modes must be
versioned; editing an active writer mode creates a new draft or dry-run version.

High-risk modes such as finance, decision records, RFCs, compliance pages, and
runbooks should remain review-required unless they only update an explicitly
managed generated section.

## Non-Goals

This contract does not define prompt text, provider-specific LLM APIs,
destination publishing APIs, UI screens, approval UX, updater storage schema,
or legal/compliance retention beyond immutable packet snapshots.
