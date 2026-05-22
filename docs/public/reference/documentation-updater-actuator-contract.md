# Documentation Updater Actuator Contract

This contract defines how a write-capable documentation updater may use Eshu.
Eshu provides read-only documentation truth findings and evidence packets. The
external updater decides whether to draft, review, diff, and publish changes
outside Eshu.

Use this page for updater integration boundaries. For MCP tool names, see
[MCP Reference](mcp-reference.md). For the deterministic verifier package, see
`go/internal/doctruth/README.md`.

## Boundary

Eshu owns read-only documentation truth:

- documentation source, document, section, link, mention, and claim-candidate
  facts
- deterministic documentation claim verification
- durable documentation findings
- evidence packet assembly
- truth labels, freshness, ambiguity, and unsupported states
- permission-aware evidence denial

The external updater owns all write behavior:

- LLM provider adapters and customer-managed API keys
- prompt templates, writer modes, and style profiles
- deterministic patch planning
- bounded LLM drafting
- deterministic and optional semantic verification
- diff rendering
- approval workflow
- destination publisher adapters
- publication audit logs

Eshu core must not:

- store LLM provider API keys
- call LLM providers for write workflows
- generate prose that is presented as source truth
- publish or mutate Confluence, Notion, Google Docs, Git repositories,
  Backstage, Jira, or any other documentation destination
- treat documentation text as more authoritative than source evidence

## Current Read APIs

The documentation read routes are mounted by
`go/internal/query/documentation.go`. MCP exposes the same routes through
`go/internal/mcp/tools_documentation.go`.

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

Use this route to find documentation findings that are safe for the caller to
inspect. The read model filters out findings when source visibility is denied,
unknown, or not evaluated.

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
- `updated_since` as an RFC3339 timestamp
- `limit` as an integer from `1` through `200`
- `cursor` as a non-negative integer offset returned by `next_cursor`

The response returns `findings` and `next_cursor`. Each finding includes the
finding identity, type, status, truth/freshness state, source/document/section
identity, summary, and evidence-packet URL.

## List Documentation Facts

`GET /api/v0/documentation/facts`

Use this route for audit and inspection of collected documentation facts. Do
not use raw facts as the updater's write basis when an evidence packet exists.
The updater's immutable snapshot should be the evidence packet.

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
- `updated_since` as an RFC3339 timestamp
- `limit` as an integer from `1` through `200`
- `cursor` as a non-negative integer offset returned by `next_cursor`

Except for `fact_kind=source`, the request must include at least one of
`scope_id`, `source_id`, `document_id`, or `section_id`.

## Get Evidence Packet

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet`

Use this route before planning any updater diff. The updater must not infer
write context by issuing arbitrary private graph queries.

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

The packet is the updater's write-planning source. Save it before drafting and
use freshness checks before publishing.

## Check Packet Freshness

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness?packet_version={saved_packet_version}`

Use this route immediately before publishing a diff. Pass the packet version
from the updater's immutable evidence snapshot.

If the saved version differs from the latest packet version, Eshu returns
`freshness_state: "stale"` with the latest version. The route still returns
`200`; the updater must enforce stale-packet policy.

```json
{
  "packet_id": "doc-packet:service-deployment:1",
  "packet_version": "1",
  "freshness_state": "stale",
  "latest_packet_version": "2"
}
```

## Error States

Current documentation routes use these stable error shapes.

| HTTP status | Error code | Meaning | Updater behavior |
| --- | --- | --- | --- |
| `400` | `invalid_argument` | Invalid timestamp, pagination, fact kind, or missing required scope. | Fix the request before retrying. |
| `403` | `permission_denied` | Caller cannot view the requested source, finding, packet, or freshness result. | Do not draft. |
| `404` | `not_found` | Finding or packet does not exist. | Drop or refresh the item. |
| `500` | `internal_error` | Eshu failed unexpectedly. | Retry according to updater policy. |
| `501` | `documentation_read_model_unavailable` | The runtime cannot serve documentation routes because the Postgres read model is not wired. | Disable drafting and alert the operator. |
| `501` | `unsupported_capability` | The current runtime profile does not support durable documentation routes. | Use a fuller Eshu profile before drafting. |

Non-envelope error bodies include:

- `error_code`
- `message`
- `correlation_id`
- `capability` when the error is capability-specific

When the caller sends `Accept: application/eshu.envelope+json`, Eshu returns
the same error in the canonical response envelope.

## Immutable Evidence Snapshots

Before drafting, the updater must save the exact evidence packet it used.

The snapshot must include the raw packet body, packet hash, Eshu base URL,
finding ID/version, packet ID/version, updater run ID, writer mode ID/version,
destination target ID, actor or service identity, and created timestamp.

The updater may store additional fields, but it must not rewrite the saved
packet after drafting starts. If Eshu later returns a newer packet version, the
updater must create a new run instead of modifying the old snapshot.

## Permission Expectations

Eshu decides whether the caller can read Eshu evidence. The updater decides
whether it can write to the destination.

Eshu must:

- enforce read permissions for findings and evidence packets
- deny evidence when source visibility is unknown or source ACLs were not
  evaluated
- include permission state in the packet
- avoid exposing destination write tokens

The updater must:

- verify destination write permission before rendering a publishable action
- keep destination credentials outside Eshu
- honor review-required policy for protected writer modes
- record who approved or published a change

Confluence is collected through read-only credentials. Source and document ACL
summaries are evidence for security review; they are not write permission
grants.

## Updater Policy Requirements

An updater must treat Eshu evidence as the source of stale-state detection.
The LLM must not decide whether documentation is stale.

Minimum updater flow:

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

Writer modes must be versioned. Editing an active writer mode creates a new
version that returns to draft or dry-run state.

High-risk modes such as finance, decision records, RFCs, compliance pages, and
runbooks should remain review-required unless they only update an explicitly
managed generated section.

## Non-Goals

This contract does not define:

- prompt text
- provider-specific LLM APIs
- destination publishing APIs
- UI screens
- approval UX
- storage schema for the updater service
- legal or compliance retention policy beyond immutable packet snapshots

Those belong to the external updater.
