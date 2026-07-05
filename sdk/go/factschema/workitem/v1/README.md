# Work Item Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload structs for the
`work_item` fact family. A decode site never reads
`Envelope.Payload["key"]` for these kinds directly; it decodes through the
parent `factschema` package's kind-keyed seam (for example
`factschema.DecodeWorkItemRecord`) and receives one of these structs,
validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)
- Directory name follows the FACT-KIND namespace (`work_item`), not the
  collector package name (`jira`) — mirrors `incident/v1`.

## Decode site is the query layer, not the reducer

Unlike most families in this module, no reducer or projector domain consumes
`work_item.*` payloads. The only decode site is the query read-model layer:
`go/internal/query/work_item_evidence_store.go` /
`work_item_evidence.go` (8 of the 9 kinds, `GET /api/v0/work-items/evidence`
and MCP `list_work_item_evidence`) and
`go/internal/query/incident_context_review_store.go` (4 kinds, incident
context review). The #4573 payload-usage manifest gate's `QueryDir` input
covers this seam the same way `ProjectorDir` covers the projector's
oci_registry decode sites.

## First DOTTED family alongside incident/kubernetes_live/oci_registry

The wire strings carry namespace dots (for example `work_item.record`); the
dots are a property of the wire kind the collector already emits
(`go/internal/facts.WorkItemRecordFactKind == "work_item.record"`). This
package matches them byte-for-byte and never invents or renames the
namespace. The schema filename is the dotted kind plus `.v1.schema.json`
(`work_item.record.v1.schema.json`).

## Purpose

Nine work-item fact kinds decode through this package, all emitted by the
Jira collector (`go/internal/collector/jira`):

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `work_item.record` | `WorkItemRecord` | `factschema.DecodeWorkItemRecord` |
| `work_item.transition` | `WorkItemTransition` | `factschema.DecodeWorkItemTransition` |
| `work_item.external_link` | `WorkItemExternalLink` | `factschema.DecodeWorkItemExternalLink` |
| `work_item.project_metadata` | `WorkItemProjectMetadata` | `factschema.DecodeWorkItemProjectMetadata` |
| `work_item.issue_type_metadata` | `WorkItemIssueTypeMetadata` | `factschema.DecodeWorkItemIssueTypeMetadata` |
| `work_item.status_metadata` | `WorkItemStatusMetadata` | `factschema.DecodeWorkItemStatusMetadata` |
| `work_item.workflow_metadata` | `WorkItemWorkflowMetadata` | `factschema.DecodeWorkItemWorkflowMetadata` |
| `work_item.field_metadata` | `WorkItemFieldMetadata` | `factschema.DecodeWorkItemFieldMetadata` |
| `work_item.metadata_warning` | `WorkItemMetadataWarning` | `factschema.DecodeWorkItemMetadataWarning` |

## Required vs optional

A field is required exactly when its json tag carries no `omitempty` (and, by
the flat-struct convention, is non-pointer). The required set of each struct is
grounded in the actual collector emitters
(`go/internal/collector/jira/envelope.go`, `envelope_metadata.go`):

- `WorkItemRecord`: required `provider`, `provider_work_item_id`,
  `work_item_key` — the emitter rejects a blank issue id or key.
- `WorkItemTransition`: required `provider`, `provider_changelog_id` only —
  the emitter rejects a blank changelog id, but does not validate
  `provider_work_item_id`/`work_item_key` as non-blank.
- `WorkItemExternalLink`: required `provider` only — the emitter's identity
  guard accepts `link.ID`, `link.GlobalID`, OR a URL-derived fingerprint as
  the source record id, so no single payload field among
  `provider_remote_link_id`/`global_id`/`url_fingerprint` is unconditionally
  present.
- `WorkItemProjectMetadata`: required `provider` only — the emitter rejects a
  payload only when BOTH `project_id` and `project_key` are blank, so neither
  is individually required.
- `WorkItemIssueTypeMetadata`: required `provider`, `issue_type_id`.
- `WorkItemStatusMetadata`: required `provider`, `status_id`.
- `WorkItemWorkflowMetadata`: required `provider`, `workflow_id`. Carries
  nested typed lists `statuses` (`WorkItemWorkflowStatus`) and `transitions`
  (`WorkItemWorkflowTransition`).
- `WorkItemFieldMetadata`: required `provider` ONLY — the Go-level emitter
  guard rejects a blank field id, but the payload's own `field_id` key is
  ALWAYS emitted as the redacted empty string; only `field_id_fingerprint`
  carries the derived identity. Promoting `field_id` to required would
  dead-letter every valid field-metadata fact.
- `WorkItemMetadataWarning`: required `provider`, `metadata_type`, `reason`.

See each struct's godoc for the full field list and the emitter grounding.

## Redaction is load-bearing (jira_work_item_v1)

The Jira collector redacts at EMISSION time
(`go/internal/collector/jira/envelope.go`, `envelope_metadata.go`): every
persisted payload carries presence booleans, bounded category enums,
URL/text sha256 fingerprints, ids/keys, and timestamps — raw summaries,
descriptions, user identities, and URLs are always emitted as the empty
string. Because redaction happens before the fact is ever persisted, a typed
struct at the read-side CANNOT re-expose anything the collector already
redacted. These redacted-to-empty fields are still declared in the structs
(documented as redacted) so the encode round-trip matches the collector's
emitted shape exactly — dropping them would silently narrow the schema
relative to what the collector emits, which is itself a contract violation
(§3.1's "no narrowing without a major bump").

`TestWorkItemEnvelopesRedactPrivateTextUsersAndURLs` and
`TestWorkItemProjectAndFieldMetadataEnvelopesRedactPrivateValues`
(`go/internal/collector/jira`) are the guard tests that prove the collector
emission side stays redacted; they are unchanged by this migration.

## Manifest-gate blind spot

Some work-item payload fields are read only by raw-SQL-JSONB queries in
`go/internal/query` (`work_item_evidence_sql.go`,
`incident_context_review_sql.go`), which the #4573 payload-usage manifest
gate's decode-seam scan cannot see on its own. The query-side seam file
(`go/internal/query/factschema_decode_workitem.go`) and the `QueryDir` gate
input close this gap for the typed-decode call sites; the
`go/internal/storage/postgres` lockstep test
(`work_item_sql_schema_lockstep_test.go`) closes it for the raw
`payload->>'field'` SQL reads, mirroring the incident/vulnerability
precedent.

## Ownership boundary

This package owns the Go type definitions and JSON tags for these nine fact
kinds' payloads. It does not own decode dispatch, schema-version routing, or
required-field validation — that lives in the parent `factschema` package
(`decode.go`, `decode_workitem.go`). It does not own graph projection or
query-surface row shaping; the query handlers under `go/internal/query`
consume the decoded structs but live outside this module.
