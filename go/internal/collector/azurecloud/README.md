# internal/collector/azurecloud

Fixture-testable Azure cloud collector fact engine. It turns Azure Resource
Graph `Resources` API response pages into provider-specific
`azure_cloud_resource` source facts, parses fixture Resource Graph
`resourcechanges` pages into provenance-only `azure_resource_change` facts, and
emits explicit `azure_collection_warning` evidence for one bounded
subscription, management-group, or tenant shard.

This package is the Azure sibling of `internal/collector/awscloud`. It follows
the [Azure Cloud Collector Contract](../../../../docs/public/reference/azure-cloud-collector-contract.md)
and the [Multi-Cloud Runtime Collector Contract](../../../../docs/public/reference/multi-cloud-collector-contract.md).

## What this slice does

- Parses Resource Graph pages (`ParseResourceGraphPage`): `totalRecords`,
  `count`, `resultTruncated`, `$skipToken`, and resource rows.
- Parses Resource Graph `resourcechanges` pages
  (`ParseResourceChangesPage`): change type, change timestamp, target ARM ID,
  operation, client type, actor class, and changed property paths only.
- Normalizes raw ARM resource IDs into identity fields (`ParseARMIdentity`):
  subscription, resource group, provider namespace, fully-qualified resource
  type (including nested types), resource name, and a lowercased normalized ID
  used for stable keys.
- Redacts the provider extension object (`RedactExtension`): keys matching the
  secret or data-plane token policy are dropped (not masked) and recorded in
  `redacted_keys`. The redaction policy version is persisted on every fact.
- Emits facts (`NewResourceEnvelope`, `NewWarningEnvelope`) with the scope and
  generation contract fields: `collector_kind=azure`, collector instance,
  tenant, scope kind, provider scope, source lane, generation, fencing token,
  a deterministic stable fact key, source ref, `source_confidence=reported`,
  and provider/observed timestamps.
- Drives a bounded scan (`Collector.Collect`): walks `$skipToken` pages,
  emits one resource fact per row, emits truncation and partial-scope warnings
  as explicit evidence, emits resource-change facts only when the boundary is
  `SourceLaneResourceChanges`, and records bounded telemetry.

## What this slice does NOT do (documented follow-ups)

- No live Azure Resource Graph or ARM calls. The `PageProvider` seam is fed by
  fixtures under `testdata/`. A live ARM client adapter is a follow-up.
- No `collector.Source`/`ClaimedService` runtime wiring, no `cmd/` binary, no
  Helm values, environment variables, or runtime profiles. The contract forbids
  claiming Azure runtime before it is implemented and fixture-proven.
- No reducer admission, no `cloud_resource_uid` resolution, no graph projection,
  no API/MCP readback. Those are reducer-owned follow-ups.
- `azure_cloud_resource` and `azure_collection_warning` are always emitted.
  `azure_tag_observation` is emitted **only when a redaction key is configured**:
  `Collector` takes an optional `WithRedactionKey`, and `emitPageResources`
  pairs each tagged resource with one tag-evidence fact whose values are keyed
  fingerprints (`FingerprintTagValues`); without a key, tag values are never
  fingerprinted or carried and no tag observation fact is emitted. The runtime
  source threads the key from `azureruntime.Source.RedactionKey`, loaded by the
  `collector-azure-cloud` binary from `ESHU_AZURE_REDACTION_KEY_FILE` (a blank
  or unreadable configured file fails closed). The `azure_cloud_relationship`
  envelope builder (`NewRelationshipEnvelope`) now exists and is unit-proven —
  provenance-only: it preserves both endpoint ARM identities, the relationship
  type, and a bounded support state, resolving no endpoints and writing no graph
  edge — but is not yet wired into the scan loop (#2197). The
  `azure_identity_observation` envelope builder
  (`NewIdentityObservationEnvelope`) is also implemented and unit-proven: it
  fingerprints every principal/client/object/tenant GUID with the redaction key
  (raw GUIDs never persist) and carries the bounded identity type, role class,
  and assignment scope as policy evidence only, with a stable key independent of
  the redaction key so key rotation does not split rows. The
  `azure_resource_change`, `azure_dns_record`, and `azure_image_reference`
  envelope builders (`NewResourceChangeEnvelope`, `NewDNSRecordEnvelope`,
  `NewImageReferenceEnvelope`) now also exist and are unit-proven — change
  records carry changed property paths plus a fingerprinted actor (a delete is a
  tombstone candidate only); DNS records fingerprint the record name and every
  target; image references are digest-first with a fingerprinted container name.
  **All Azure source fact-family envelope builders are now implemented.** The
  scan loop also emits, from existing parsed ARM fields, a provenance-only
  `azure_cloud_relationship` (`managed_by`) from each resource's `managedBy`
  owning-resource reference (no redaction key needed), and a keyed
  `azure_identity_observation` from each `identity` block — both the
  system-assigned identity and one per user-assigned identity under
  `userAssignedIdentities` (principal/client/tenant GUIDs fingerprinted, emitted
  only when a redaction key is set). The scan loop now also emits
  `azure_resource_change` from fixture `resourcechanges` pages when the boundary
  source lane is `SourceLaneResourceChanges`; changed actors are fingerprinted,
  changed property values and raw provider bodies are dropped, and delete
  records remain tombstone candidates only. What remains per kind is emission
  for the families whose source data still needs the live transport (DNS records
  and image references) and reducer admission. Reducer admission of tag evidence
  is already wired (#2192).

## Invariants

- Raw ARM resource IDs are preserved; normalized fields are additive.
- Extension payloads never carry deployment templates, secrets, Key Vault
  values, connection strings, access keys, tokens, IP addresses, private
  endpoint hostnames, or provider response bodies.
- Stable fact keys derive from normalized identity, resource type, source lane,
  and tenant only, so extension or tag churn does not split idempotent
  re-emission of a generation.
- Partial scope, permission-hidden subscriptions, and truncation are explicit
  warning evidence, never silent success.
- Telemetry labels are bounded enums only.
- Resource-change facts are source provenance only. They never write graph
  truth, never promote a tombstone by themselves, and never carry before/after
  values or raw provider response bodies.

## Fixture matrix covered

`collector_test.go`, `envelope_test.go`, `redaction_test.go`,
`resourcegraph_test.go`, `resourcechanges_test.go`, `armid_test.go`, and
`metrics_test.go` cover:
skip-token pagination resume, idempotent re-emission of the same generation,
stale/invalid generation rejection, truncation warning, partial-scope and
permission-hidden warning accounting, malformed-row unsupported warning,
extension redaction (including nested maps), provider-read-error propagation,
resource-change parsing/emission, resource-change empty state, actor
fingerprinting, raw before/after value exclusion, and bounded telemetry-label
safety.

## Tests

```bash
cd go && go test ./internal/collector/azurecloud/... ./internal/facts/ -count=1
```

## Performance and Observability Evidence

Collector Performance Evidence: this slice adds isolated fixture-driven
resource-change parsing and changes no existing graph or storage hot path.
Baseline: Azure resource-change emission did not exist. After: bounded
in-memory normalization of Resource Graph pages with no Cypher, graph writes,
Postgres writes, worker/lease/queue changes, or claim-driven scheduler changes.
Input shape is fixture pages resumed by skip-token; work is O(change rows x
pages), and terminal output is a bounded generation of `azure_resource_change`
facts plus explicit warning facts for unsupported rows or partial scope. Focused
proof: `go test ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/... ./internal/facts/ -count=1`.

Collector Observability Evidence: the package exports bounded-label data-plane metrics
`eshu_dp_azure_api_calls_total`, `_skip_token_resumes_total`,
`_partial_scope_total`, `_facts_emitted_total`, and `_freshness_lag_seconds`.
Labels are bounded enums only (collector kind, scope kind, source lane,
operation, status class, fact kind, outcome); a test asserts no ARM resource id,
subscription id, tenant id, resource name, tag, or URL appears in any label. An
operator reads partial-scope coverage, skip-token resumes, freshness lag,
source-lane labels, and fact counts to answer whether a scan is complete,
fresh, throttled, or partial. Resource-change actors are fingerprinted with the
configured redaction key; raw actors, before/after values, and provider bodies
are absent from emitted facts.

No-Observability-Change: no telemetry contract file changes are needed. The
resource-change lane reuses the existing Azure bounded metric family, adds
`source_lane=resource_changes` and `operation=resource_changes_list`, and keeps
all identifiers out of metric labels.

Collector Deployment Evidence: no Docker Compose service, Helm Deployment,
Service, ServiceMonitor, port, chart value, runtime profile, or live Azure
credential path changes in this slice. Resource-change emission stays behind
fixture provider injection and `SourceLaneResourceChanges`.
