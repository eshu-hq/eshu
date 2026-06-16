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

## What this package does NOT do

- No live Azure Resource Graph or ARM calls. The `PageProvider` seam is fed by
  fixtures under `testdata/`. The live client remains gated in the sibling
  `azureruntime` package.
- No durable commit, claim scheduling, Helm values, chart wiring, runtime
  profiles, reducer admission, graph projection, or API/MCP readback. Runtime
  wiring lives in `azureruntime` and `cmd/collector-azure-cloud`; reducer and
  readback work lives in the reducer, storage, query, and MCP packages.

## Current fact emission

- `azure_cloud_resource` and `azure_collection_warning` are always emitted.
  `azure_tag_observation` is emitted **only when a redaction key is configured**:
  `Collector` takes an optional `WithRedactionKey`, and `emitPageResources`
  pairs each tagged resource with one tag-evidence fact whose values are keyed
  fingerprints (`FingerprintTagValues`); without a key, tag values are never
  fingerprinted or carried and no tag observation fact is emitted. The runtime
  source threads the key from `azureruntime.Source.RedactionKey`, loaded by the
  `collector-azure-cloud` binary from `ESHU_AZURE_REDACTION_KEY_FILE`; a blank
  or unreadable configured file fails closed.
- `azure_cloud_relationship` emits provenance-only `managed_by` evidence from
  each resource's ARM `managedBy` reference. The collector preserves endpoint
  ARM identities and support state; reducer packages own endpoint resolution and
  graph writes.
- `azure_identity_observation` emits keyed system-assigned and user-assigned
  managed-identity observations from each resource `identity` block when a
  redaction key is configured. Raw principal, client, object, and tenant GUIDs
  never persist.
- `azure_resource_change` emits fixture Resource Graph `resourcechanges`
  evidence when the boundary source lane is `SourceLaneResourceChanges`.
  Changed actors are fingerprinted, before/after values and raw provider bodies
  are dropped, and delete records remain tombstone candidates only.
- `azure_dns_record` emits keyed DNS record-set evidence from supported Azure
  DNS Resource Graph rows when a redaction key is configured. Record names and
  targets are fingerprinted, TTL and record type stay bounded, unsupported or
  empty record families are skipped, DNS record-set properties are not copied
  into the generic resource extension, and DNS evidence remains
  provenance-only.
- `azure_image_reference` emits keyed Container Apps image evidence from the
  safe `properties.template.containers` shape when a redaction key is
  configured. Duplicate image references collapse within a row, container names
  are fingerprinted, tag-only references stay lower-confidence evidence, and
  owning ARM resources are not promoted into workload or service truth.

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
`resourcegraph_test.go`, `resourcechanges_test.go`,
`source_lane_emission_test.go`, `armid_test.go`, and `metrics_test.go` cover:
skip-token pagination resume, idempotent re-emission of the same generation,
stale/invalid generation rejection, truncation warning, partial-scope and
permission-hidden warning accounting, malformed-row unsupported warning,
extension redaction (including nested maps), provider-read-error propagation,
resource-change parsing/emission, resource-change empty state, actor
fingerprinting, raw before/after value exclusion, DNS/image source-lane
emission, no-key fail-closed behavior, unsupported/empty source-lane skips,
duplicate image-reference convergence, and bounded telemetry-label safety.

## Tests

```bash
cd go && go test ./internal/collector/azurecloud/... ./internal/facts/ -count=1
```

## Performance and Observability Evidence

Collector Performance Evidence: this package adds isolated fixture-driven
resource-change parsing plus DNS and Container Apps image-reference source-lane
extraction, and changes no existing graph or storage hot path. Baseline: later
Azure source families had envelope builders only. After: bounded in-memory
normalization of Resource Graph pages with no Cypher, graph writes, Postgres
writes, worker/lease/queue changes, or claim-driven scheduler changes. Input
shape is fixture pages resumed by skip-token; work is O(rows x page count) with
small per-row extraction for supported source families. Focused proof:
`go test ./internal/collector/azurecloud -count=1`.

Collector Observability Evidence: the package exports bounded-label data-plane metrics
`eshu_dp_azure_api_calls_total`, `_skip_token_resumes_total`,
`_partial_scope_total`, `_facts_emitted_total`, and `_freshness_lag_seconds`.
Labels are bounded enums only (collector kind, scope kind, source lane,
operation, status class, fact kind, outcome); a test asserts no ARM resource id,
subscription id, tenant id, resource name, tag, or URL appears in any label. An
operator reads partial-scope coverage, skip-token resumes, freshness lag,
source-lane labels, and fact counts to answer whether a scan is complete,
fresh, throttled, or partial. Resource-change actors, DNS names and targets,
and container names are fingerprinted with the configured redaction key; raw
actors, DNS targets, container names, before/after values, and provider bodies
are absent from the new emitted facts.

No-Observability-Change: no telemetry contract file changes are needed. The
resource-change lane reuses the existing Azure bounded metric family, adds
`source_lane=resource_changes` and `operation=resource_changes_list`, and keeps
all identifiers out of metric labels. DNS and image source-lane emission reuse
the existing fact-count metric keyed only by bounded fact kind.

Collector Deployment Evidence: no Docker Compose service, Helm Deployment,
Service, ServiceMonitor, port, chart value, runtime profile, or live Azure
credential path changes in this slice. Resource-change, DNS, and image
source-lane emission stay behind fixture provider injection or already-gated
live provider injection.
