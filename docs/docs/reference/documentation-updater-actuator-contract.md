# Documentation Updater Actuator Contract

This contract defines how a write-capable documentation updater may use Eshu.
It keeps the boundary simple: Eshu reports documentation truth findings and
evidence packets. The updater decides whether to draft, review, diff, and
publish changes outside Eshu.

The contract applies to future updater services for Confluence, Git-backed
Markdown, Notion, Google Docs, Backstage, ADRs, RFCs, finance pages, runbooks,
and similar documentation systems.

## Boundary

Eshu owns read-only truth:

- documentation source collection
- document, section, link, mention, and claim-candidate facts
- drift findings
- evidence packet assembly
- truth labels, freshness, ambiguity, and unsupported states
- permission-aware evidence redaction

The external updater owns write behavior:

- LLM provider adapters and customer-managed API keys
- prompt templates and writer modes
- style profiles
- deterministic patch planning
- bounded LLM drafting
- verification
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

## Required Eshu APIs

The API names below define the Eshu-side contract for updater actuators. These
routes return read-only evidence; they do not draft text or write to destination
systems.

### List Findings

`GET /api/v0/documentation/findings`

Returns documentation findings the updater may inspect. Eshu filters findings
through the same documentation visibility rules used for evidence packets, so
denied source or document metadata is not exposed in the list response.

Required filters:

- `finding_type`
- `source_id`
- `document_id`
- `status`
- `truth_level`
- `freshness_state`
- `updated_since`
- `limit` as an integer from `1` through `200`
- `cursor` as a non-negative integer offset returned by `next_cursor`

Response shape:

```json
{
  "findings": [
    {
      "finding_id": "doc-finding:service_deployment_drift:...",
      "finding_version": "2026-05-09T19:00:00Z",
      "finding_type": "service_deployment_drift",
      "status": "conflict",
      "truth_level": "derived",
      "freshness_state": "fresh",
      "source_id": "doc-source:confluence:example.atlassian.net:100",
      "document_id": "doc:confluence:123",
      "section_id": "body",
      "summary": "The page says payment-service deploys from one chart, but Eshu currently sees a different deployment source.",
      "evidence_packet_url": "/api/v0/documentation/findings/doc-finding:service_deployment_drift:.../evidence-packet"
    }
  ],
  "next_cursor": ""
}
```

### Get Evidence Packet

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet`

Returns the bounded evidence the updater may snapshot before planning a diff.
The updater must not infer write context by issuing arbitrary private graph
queries.

Required fields:

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

Response shape:

```json
{
  "packet_id": "doc-packet:service_deployment_drift:...",
  "packet_version": "1",
  "generated_at": "2026-05-09T19:00:00Z",
  "finding": {
    "finding_id": "doc-finding:service_deployment_drift:...",
    "finding_version": "2026-05-09T19:00:00Z",
    "finding_type": "service_deployment_drift",
    "status": "conflict"
  },
  "document": {
    "source_id": "doc-source:confluence:example.atlassian.net:100",
    "document_id": "doc:confluence:123",
    "external_id": "123",
    "canonical_uri": "https://example.atlassian.net/wiki/spaces/PLAT/pages/123",
    "revision_id": "17",
    "title": "Payment Service Deployment"
  },
  "section": {
    "section_id": "body",
    "heading_text": "Deployment",
    "text_hash": "sha256:..."
  },
  "bounded_excerpt": {
    "text": "payment-service deploys from platform/payment-chart",
    "text_hash": "sha256:...",
    "source_start_ref": "storage:body",
    "source_end_ref": "storage:body"
  },
  "linked_entities": [
    {
      "entity_id": "service:payment-service",
      "entity_type": "service",
      "match_status": "exact",
      "confidence": "observed"
    }
  ],
  "current_truth": {
    "claim_key": "deployment_source",
    "documented_value": "platform/payment-chart",
    "current_value": "platform/payment-service/deploy",
    "truth_level": "derived",
    "freshness_state": "fresh"
  },
  "evidence_refs": [
    {
      "fact_id": "fact:...",
      "source_system": "git",
      "source_uri": "https://github.com/example/platform-deployments",
      "source_record_id": "payment-service/deploy.yaml"
    }
  ],
  "truth": {
    "label": "derived",
    "basis": "deployment graph evidence",
    "ambiguity": []
  },
  "permissions": {
    "viewer_can_read_source": true,
    "packet_redacted": false,
    "write_permission_decision": "external_updater_must_check"
  },
  "states": {
    "finding_state": "ready",
    "unsupported_reason": "",
    "stale_reason": ""
  }
}
```

### Check Packet Freshness

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness?packet_version={saved_packet_version}`

Allows the updater to check whether a saved packet is still current before it
publishes a diff. The `packet_version` query parameter should be the version
from the updater's immutable evidence snapshot. If it differs from the latest
packet version, Eshu returns `freshness_state: "stale"` with the latest version
for the caller to refetch.

Response shape:

```json
{
  "packet_id": "doc-packet:service_deployment_drift:...",
  "packet_version": "1",
  "freshness_state": "fresh",
  "latest_packet_version": "1"
}
```

## Error States

Eshu responses must use explicit states instead of vague failures.

| HTTP status | Error code | Meaning | Updater behavior |
| --- | --- | --- | --- |
| `400` | `invalid_argument` | Request parameters are invalid, such as a malformed `updated_since` timestamp. | Fix the request before retrying. |
| `401` | `unauthenticated` | Caller identity is missing or invalid. | Stop and require auth. |
| `403` | `permission_denied` | Caller cannot view the requested source, document, or evidence. | Do not draft. |
| `404` | `not_found` | Finding, packet, document, or section does not exist. | Drop or refresh the item. |
| `409` | `stale_packet` | The packet is no longer current. | Fetch the latest packet and restart planning. |
| `422` | `unsupported_finding` | Eshu cannot produce a supported packet for this finding type. | Mark unsupported. |
| `423` | `building` | Eshu is still collecting or reducing required evidence. | Retry later with backoff. |
| `429` | `rate_limited` | Caller exceeded rate limits. | Retry after the stated interval. |
| `500` | `internal_error` | Eshu failed unexpectedly. | Retry according to updater policy. |
| `501` | `documentation_read_model_unavailable` | The runtime cannot serve documentation packets because the Postgres read model is not wired. | Disable drafting and alert the operator. |
| `501` | `unsupported_capability` | The current runtime profile does not support documentation evidence packets. | Use a fuller Eshu profile before drafting. |
| `503` | `source_unavailable` | Required source evidence is temporarily unavailable. | Retry later. |

Error bodies must include:

- `error_code`
- `message`
- `retry_after_seconds` when retryable
- `correlation_id`

## Immutable Evidence Snapshots

Before drafting, the updater must save the exact evidence packet it used.

The snapshot must include:

- raw packet body
- packet hash
- Eshu base URL
- finding ID and version
- packet ID and version
- updater run ID
- writer mode ID and version
- destination target ID
- actor or service identity
- created timestamp

The updater may store additional fields, but it must not rewrite the saved
packet after drafting starts. If Eshu later returns a newer packet version, the
updater must create a new run instead of modifying the old snapshot.

## Permission Expectations

Eshu decides whether the caller can read Eshu evidence. The updater decides
whether it can write to the destination.

Eshu must:

- enforce read permissions for findings and evidence packets
- redact or deny evidence that the caller cannot view
- include redaction state in the packet
- avoid exposing destination write tokens
- treat missing or unknown evidence visibility as denied

Documentation evidence packets must include an explicit permission decision.
If `permissions.viewer_can_read_source` is absent or false, Eshu must not return
the packet body or any bounded excerpt. If a source reports only partial ACL
metadata, the packet producer must either prove the caller can read the source
or mark the packet denied.

The updater must:

- verify destination write permission before rendering a publishable action
- keep destination credentials outside Eshu
- honor review-required policy for protected writer modes
- record who approved or published a change

Confluence is currently collected through read-only credentials. The collector
records source and document ACL summaries as partial when page restrictions are
not fetched from the source API. That metadata is evidence for security review;
it is not a write permission grant.

## Updater Policy Requirements

An updater must treat Eshu evidence as the source of stale-state detection.
The LLM must not decide whether documentation is stale.

The minimum updater flow is:

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

High-risk modes such as finance, ADR, RFC, compliance, and runbook writers
should remain review-required unless they only update an explicitly managed
generated section.

## Non-Goals

This contract does not define:

- prompt text
- provider-specific LLM APIs
- destination publishing APIs
- UI screens
- approval UX
- storage schema for the updater service
- legal or compliance retention policy beyond immutable packet snapshots

Those belong to the external updater repo.

## Follow-Up Work

- Implement the evidence packet API in Eshu (#71).
- Implement `service_deployment_drift` findings (#65).
- Create the external updater repository and file implementation issues there.
- Add security review for writer modes, BYOK handling, destination publishers,
  and immutable audit storage.
