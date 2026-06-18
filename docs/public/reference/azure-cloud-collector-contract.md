# Azure Cloud Collector Contract

This page defines the provider-specific design baseline for a future Azure cloud
collector. It is a child of the
[Multi-Cloud Runtime Collector Contract](multi-cloud-collector-contract.md) and
is not a deployed runtime promise.

The collector kind is `azure`. It observes Azure control-plane metadata through
Azure Resource Graph and bounded Azure Resource Manager fallback reads, then
emits source facts only. Reducers own canonical `CloudResource` identity,
drift, unmanaged-resource detection, relationship graph writes, and API or MCP
truth.

## Status

The first fixture-testable slice landed in `go/internal/collector/azurecloud`.
It registers the Azure fact constants and schema versions in
`go/internal/facts/azure.go`, normalizes ARM resource identity, redacts the
provider extension payload, and emits `azure_cloud_resource` and
`azure_collection_warning` source facts from fixture Resource Graph pages.

The runtime scaffolding slice (issue #1998) has now landed in
`go/internal/collector/azurecloud/azureruntime` and
`go/cmd/collector-azure-cloud`. It adds:

- the `azure` scope `CollectorKind` (`scope.CollectorAzure`),
- a non-claimed `collector.Source` (`azureruntime.Source`) that yields one
  collected generation per declarative tenant/subscription/management-group
  scope target by reading Resource Graph pages through the existing
  `PageProvider` seam with `$skipToken` resume,
- the `collector-azure-cloud` binary wiring that source into the shared
  `collector.Service` for atomic fact + generation commit, with credentials
  referenced by name only.

The **fixture mode** default still performs **no live Azure calls**. The live
Resource Graph client is a documented seam (`azureruntime.LiveProviderFactory`)
that is gated by construction: its zero value returns `ErrLiveProviderGated`, and
live Resource Graph reads require explicit injection of a read-only client. The
owned default live query avoids the full ARM `properties` bag, SDK rows are
capped before conversion, and throttling, permission-hidden scopes, and expired
auth or continuation tokens surface as warning evidence. Tests and the binary's
fixture mode use a fixture or file-backed `PageProvider`.

Claimed-live transport is now wired and **off by default**: the
`collector-azure-cloud` binary `-mode claimed-live` path (issue #3024) selects an
enabled, claim-enabled `azure` instance with `live_collection_enabled=true`,
resolves the ambient read-only Azure credential, injects the SDK Resource Graph
client into `LiveProviderFactory`, and runs through `collector.ClaimedService`.
The claimed source serves the `resource_graph` lane only. Default-off Helm
exposure (deployment, metrics service, ServiceMonitor, render-time validation)
mirrors GCP. **Live smoke proof against a real tenant remains gated**; promotion
to `implemented` requires an operator-run live proof.

The allowlisted ARM fallback seam is also implemented behind
`azureruntime.LiveProviderFactory`. It remains non-default and requires explicit
in-process injection of a separate read-only `LiveARMFallbackClient`, exact
resource-type `LiveARMFallbackRule` entries, fixed API versions, and bounded
extension fields. The SDK wrapper exposes only Azure Resource Manager
`GetByID`, and the fallback never registers providers or mutates resources. A
fallback payload is attached under the Resource Graph row extension as
`armFallback`, carries its own schema version, is byte-bounded before
attachment, and then passes through the existing `azure_cloud_resource`
redaction policy before persistence. Unsupported families, throttles, timeouts,
permission-hidden resources, and oversized fallback payloads emit
`azure_collection_warning` evidence rather than silent empty success.

Shared multi-cloud reducer admission and API/MCP readback for the
`azure_cloud_resource` identity are now implemented and fixture-proven. The
additive `cloud_inventory_admission` reducer domain
(`go/internal/reducer/cloud_inventory_admission.go`, wired in
`go/cmd/reducer`) admits `azure_cloud_resource` into the shared `CloudResource`
identity keyspace (`cloud_resource_uid`) as the reducer-owned
`reducer_cloud_resource_identity` read model, keying on the
`arm_resource_id` via deterministic provider identity resolution
(`go/internal/correlation/cloudinventory`), with
declared-over-applied-over-observed management-origin precedence,
ambiguous/unsupported/unresolved accounting, and stale-generation supersession.
The `GET /api/v0/cloud/inventory` handler
(`go/internal/query/cloud_inventory_readback.go`) and the
`list_cloud_resource_inventory` MCP tool return bounded, truth-labeled Azure
identity rows (exact, semantic-facts basis) without leaking raw provider
locators.

`azure_tag_observation` is now fully wired: the collector emits keyed
tag-value-fingerprint facts behind a redaction key, and the shared
`cloud_inventory_admission` reducer domain attaches those fingerprints onto the
canonical resource sharing their `cloud_resource_uid` (tags never admit a
resource on their own), surfaced as `tag_value_fingerprints` on the
`GET /api/v0/cloud/inventory` readback. Tag value text never crosses the wire;
only the keyed markers do.

`azure_identity_observation` now feeds the same shared
`cloud_inventory_admission` reducer domain as additive identity-policy
evidence. Identity observations attach only to the canonical Azure resource
sharing their `cloud_resource_uid`; they never admit a resource, create graph
nodes, or mint IAM edges on their own. The readback surfaces bounded
`identity_policy_evidence` rows containing the stable evidence key, bounded
identity/role classes, and keyed principal/client/object/tenant fingerprints.
Raw ARM identities, assignment scopes, and raw principal GUIDs never cross the
API or MCP boundary. Rows are capped per resource and set
`identity_policy_evidence_truncated` when evidence was bounded.

`azure_resource_change` is wired as freshness evidence only. The shared
`cloud_inventory_admission` reducer domain reads persisted Resource Graph change
facts for the same scope generation and attaches sanitized rows only to
resources already admitted by inventory evidence. Change-only facts cannot mint
canonical resources, write graph state, or finalize deletes; delete records
remain `tombstone_candidate` investigation hints until inventory or reducer
evidence confirms final state. The `GET /api/v0/cloud/inventory` readback and
the MCP inventory tool expose only bounded change metadata, actor
class/fingerprint, property paths, and truncation/tombstone flags; raw target
ARM IDs, raw actors, provider bodies, and before/after values are not echoed.

`azure_image_reference` facts now feed the existing `container_image_identity`
handoff and reducer path when such facts are present. The projector enqueues the
existing image-identity reducer domain for Azure image-reference generations,
the reducer keeps digest-first behavior, and the active Postgres image-identity
loader includes Azure image-reference facts. Azure owning ARM identities remain
source evidence only; they do not mint repository, workload, service, or graph
truth.

`azure_cloud_relationship` facts now feed fixture/offline graph
materialization for managed ARM relationships. The projector enqueues the Azure
resource-node and relationship-edge reducer domains from committed source facts.
The reducer materializes only observed `azure_cloud_resource` facts into shared
`CloudResource` nodes, publishes the canonical-nodes readiness phase, and then
resolves relationship endpoints by exact normalized ARM resource ID. This slice
admits only the bounded `managed_by` relationship type, written as
`AZURE_managed_by` only when both endpoint nodes exist; missing, partial,
unsupported, invalid, self-loop, stale, and unresolved rows are counted and
skipped. The Cypher writer uses MATCH-MATCH-MERGE over existing
`CloudResource.uid` endpoints and keeps the raw relationship type as a property
for graph readback. It does not call Azure, mint target nodes from relationship
facts, or activate API/MCP readback lanes.

Claimed-live command wiring and default-off Helm exposure now exist for the
Resource Graph lane (issue #3024). Live smoke proof against a real tenant, the
hosted security posture sign-off, ARM-fallback live activation, and API/MCP
readback promotion remain gated until their own proof lands. The
`azure_cloud_relationship` envelope builder
(`NewRelationshipEnvelope`) is implemented and unit-proven as provenance-only
(both endpoint ARM identities, relationship type, and a bounded support state;
it resolves no endpoints in the collector). The
`azure_identity_observation` envelope builder (`NewIdentityObservationEnvelope`)
is implemented and unit-proven: it fingerprints every
principal/client/object/tenant GUID with the redaction key (raw GUIDs never
persist) and carries the bounded identity type, role class, and assignment scope
as policy evidence only. The `azure_resource_change`, `azure_dns_record`, and
`azure_image_reference` envelope builders (`NewResourceChangeEnvelope`,
`NewDNSRecordEnvelope`, `NewImageReferenceEnvelope`) are implemented and
unit-proven: change records carry changed property paths plus a fingerprinted
actor (a delete is a tombstone candidate only); DNS records fingerprint the
record name and every target; image references are digest-first with a
fingerprinted container name. Scan-loop emission now exists for DNS record-set
rows and Container Apps image references when a redaction key is configured:
unsupported or empty source data is skipped, duplicate image references converge
within a row, DNS evidence remains provenance-only, and owning ARM resources do
not mint repository, workload, service, deployment, or graph truth. DNS
record-set properties are not persisted in the generic resource extension;
their safe evidence is carried only by `azure_dns_record`. Reducer admission
for DNS follows.

The implemented slices remain fixture-testable without live Azure access.
Live smoke tests are promotion proof, not the minimum proof for the source
contract.

No-Regression Evidence: `go test ./internal/reducer ./internal/query
./internal/mcp -run CloudInventory -count=1` and `go test
./internal/storage/postgres -run CloudInventory -count=1` prove
`azure_cloud_resource` admission into the shared `cloud_resource_uid` keyspace,
management-origin precedence, ambiguous/unsupported/unresolved accounting,
stale-generation supersession, and bounded truth-labeled readback that never
leaks raw provider locators. `go test ./internal/reducer ./internal/storage/postgres
./internal/query -run 'CloudTag|CloudInventoryAdmissionAttachesTagEvidence|CloudInventoryAdmissionWithoutTagLoader|ResourceViewSurfacesTagFingerprints|ResourceViewOmitsTagsWhenAbsent' -count=1`
proves tag-evidence fingerprints attach only to the resource sharing their uid,
never admit a resource on their own, leave the AWS/GCP path unchanged when no
tag loader is configured, and surface as `tag_value_fingerprints` on the
readback without exposing tag value text. `go test ./internal/storage/postgres
-run CloudIdentityPolicyEvidence -count=1`, `go test ./internal/reducer -run
'CloudInventoryAdmission(AttachesIdentityPolicyEvidence|CapsIdentityPolicyEvidence)|PostgresCloudInventoryAdmissionWriterPersistsOneFactPerResource'
-count=1`, and `go test ./internal/query -run
TestCloudInventoryResourceViewSurfacesBoundedIdentityPolicyEvidence -count=1`
prove identity-policy evidence loads from Azure identity facts, drops malformed
or orphan rows, attaches only to admitted resources, caps per-resource payloads,
and exposes only bounded classes plus keyed fingerprints on readback. `go test
./internal/projector -run
TestBuildProjectionQueuesContainerImageIdentityForAzureImageReference -count=1`
proves Azure image-reference generations enqueue the existing
`container_image_identity` reducer domain. `go test ./internal/reducer -run
'Test(ContainerImageIdentityFactKindsIncludesAzureImageReferences|BuildContainerImageIdentityDecisions(ConsumesAzureDigestReference|ResolvesAzureTagOnlyWithRegistryEvidence))'
-count=1` proves `azure_image_reference` facts are loaded by the
image-identity reducer, explicit Azure digests become exact digest-keyed
identity decisions, tag-only Azure facts require OCI registry tag evidence, and
the owning ARM resource identity does not invent repository anchors. `go test
./internal/storage/postgres -run
TestFactStoreListActiveContainerImageIdentityFactsUsesActiveIdentityGenerations
-count=1` proves the active cross-scope image-identity fact loader includes
Azure image-reference facts while preserving active-generation and tombstone
predicates. `go test ./internal/collector/azurecloud -run
'TestCollect(EmitsDNSAndImageReferencesWhenKeyed|SkipsDNSAndImageReferencesWithoutKey|SourceLaneEmissionHandlesEmptyUnsupportedMalformedAndDuplicateRows|SourceLaneEmissionPreservesPartialScopeWarning)'
-count=1` proves Resource Graph scan-loop emission for keyed DNS and Container
Apps image source rows, no-key fail-closed behavior, unsupported and empty
source-data skips, duplicate image-reference convergence, partial-scope warning
preservation, and DNS/container redaction boundaries without live Azure access.
`go test ./internal/reducer -run
'Azure(Resource|Relationship)Materialization|ExtractAzure' -count=1`, `go test
./internal/projector -run Azure -count=1`, `go test ./internal/storage/cypher
-run AzureCloudResourceEdgeWriter -count=1`, and `go test ./cmd/reducer -run
Azure -count=1` prove Azure resource-node readiness, exact ARM-id relationship
resolution, no dangling or fabricated edges, bounded unsafe-type handling,
skip accounting, scoped stale-edge retraction, MATCH-only Cypher endpoint
readback properties, and reducer wiring without live Azure activation or
API/MCP readback changes.

No-Observability-Change: the live Azure runtime adapter stays gated. The
runtime scaffolding slice adds a non-claimed source and binary that emit a
bounded per-target span (`collector.azure.scope_scan`) and reuse the
package-local bounded-label Azure instruments only. It adds no live Azure call,
no chart values, no shared-registry telemetry series, and no query claims. Azure
image identity admission reuses the existing projector reducer-intent handoff,
`container_image_identity` reducer execution, durable
`reducer_container_image_identity` fact writer, active OCI registry evidence
loader, active-generation fact predicates, and
`eshu_dp_container_image_identity_decisions_total` outcome counter; it adds no
live provider call, route, queue domain, worker, runtime knob, metric label,
graph write, or chart surface.
Azure relationship materialization reuses the reducer queue, bounded completion
logs, `GraphProjectionPhaseCanonicalNodesCommitted` readiness lookup, and the
existing CloudResource Cypher writer shape; it adds no live Azure call, route,
credential surface, chart value, or API/MCP tool surface.
Azure DNS and image source-lane emission reuses the collector's existing
bounded fact-count metric labels and adds no live Azure call, route, worker,
queue domain, chart value, API/MCP tool surface, or graph write.

## Source Truth

Azure Resource Graph is the baseline source for broad inventory across
subscriptions, management groups, or tenant-level scopes. Scheduled Resource
Graph inventory scans remain authoritative for completeness. Resource Graph
change data is freshness input, not a replacement for full scans.

| Source lane | Use | Eshu boundary |
| --- | --- | --- |
| Resource Graph `Resources` API | Bounded inventory and metadata queries over configured subscriptions or management groups. Use explicit KQL, `$top`, `$skipToken`, `resultTruncated`, and scope metadata. | Emits source facts for the current generation. It does not write graph truth. |
| Resource Graph tables and joins | Safe discovery for related resource classes when the table supports the relationship. | Provider relationship evidence only. Reducers resolve both endpoints before graph writes. |
| Resource Graph `resourcechanges` | Freshness hint for create, update, and delete records with change type, target resource, operation, client type, and timestamp. | Emits change evidence. Reducers attach sanitized freshness only to already-admitted resources; it does not prove final resource state and cannot replace inventory scans. |
| Azure Resource Manager fallback | Optional read-only `GET` calls for configured resource families where Resource Graph lacks safe detail. | Must be allowlisted, bounded, read-only, and versioned separately in the source fact payload. |

Important provider constraints:

- Resource Graph returns only resources the principal can read. Partial
  subscription or management-group access must be explicit.
- Resource Graph queries are KQL. Query strings are implementation metadata and
  must not appear in metric labels or user-facing status.
- Resource Graph throttles queries and exposes quota reset information in
  response headers. Requests need bounded concurrency, retry, and backoff.
- Live Resource Graph response rows must stay bounded before persistence. The
  owned default query must not project the full ARM `properties` bag; any
  explicitly overridden query still needs a per-row payload cap.
- `resourcechanges` records are freshness evidence. Delete records can lack full
  snapshots, so deletion handling must stay conservative.
- The collector must not register resource providers, deploy resources, delete
  resources, or mutate Azure state. Resource-provider registration is an
  operator action, not scanner behavior.

## Scope And Generation

Azure collector claims should shard by configured tenant, subscription or
management group, resource type family, location bucket, and source lane:

```text
(collector_instance_id, tenant_id, scope_kind, scope_id, resource_type_family, location_bucket, source_lane)
```

| Field | Contract |
| --- | --- |
| `collector_kind` | Always `azure`. |
| `collector_instance_id` | Configured runtime instance that owns target policy and credential environment. |
| `scope_id` | Stable Eshu scope for the Azure shard. Prefer `azure:<tenant_id>:<scope_kind>:<scope_id>:<resource_family>:<location_bucket>:<source_lane>`. |
| `tenant_id` | Azure tenant ID or tenant fingerprint, depending on retention policy. |
| `scope_kind` | `subscription`, `management_group`, or `tenant`. |
| `provider_scope_id` | Subscription ID, management group ID, or tenant scope fingerprint as source evidence. |
| `generation_id` | Collector or coordinator assigned for one bounded scan. |
| `stable_fact_key` | Deterministic key from fact kind, ARM resource ID, resource type, source lane, and provider timestamp where present. |
| `query_checkpoint` | `$skipToken`, `$top`, truncation, and partial-scope metadata, never used as durable resource identity. |
| `observed_at` | Eshu observation time for the provider response. |
| `provider_time` | Change timestamp, Resource Graph read time when available, or explicit missing state. |

One claim per individual resource is only for repair or replay. Normal scans
should use bounded shards that can finish within the collector lease and query
quota budget.

## Fact Families

Initial Azure implementation should use provider-specific source facts, then
let reducers admit shared cloud identity. Do not introduce a catch-all source
fact unless the schema PR deliberately migrates AWS, GCP, and Azure together.

| Fact kind | Purpose | Required source fields |
| --- | --- | --- |
| `azure_cloud_resource` | One Resource Graph resource observation. | ARM resource ID, tenant/scope identity, subscription ID, resource group, provider namespace, resource type, name, location, kind, SKU class, managedBy, tags, identity presence, API version when known, redaction policy version. |
| `azure_tag_observation` | Tag evidence from Resource Graph or ARM fallback. | ARM resource ID, tag key/value fingerprints or bounded tags, source lane, read time. |
| `azure_identity_observation` | Managed identity or role/authorization metadata where safely exposed. | ARM resource ID, identity type, principal/client/tenant fingerprints, role/action class, scope, read time. |
| `azure_resource_change` | Resource Graph change evidence. | target ARM ID, target type, change type, timestamp, operation, client type, actor class/fingerprint, changed property paths, truncation flags. |
| `azure_cloud_relationship` | Provider relationship evidence from Resource Graph joins or ARM fallback. | source ARM ID, source type, relationship type, target ARM ID, target type, read/update time, support state. |
| `azure_dns_record` | DNS record metadata where Resource Graph or safe ARM APIs expose it. | zone identity, record type, record name fingerprint, target fingerprints or bounded values, TTL, read time. |
| `azure_image_reference` | Runtime image reference evidence from AKS, Container Apps, App Service, VM scale sets, or other safe metadata. | owning resource identity, image reference or digest when present, tag/digest confidence, container name fingerprint, read time. |
| `azure_collection_warning` | Explicit partial, unsupported, stale, permission-hidden, quota, fallback, truncation, or redaction outcomes. | scope, resource family, source lane, reason, outcome, retryability, read time. |

Facts must use `source_confidence=reported` for provider API data. Fixture facts
used in tests can use the same provider confidence because they model provider
responses, not local repository observations.

## Payload Boundaries

Azure payloads should preserve provider identity while avoiding private or
data-plane content:

- Keep ARM resource IDs in facts when needed for exact reducer joins.
- Keep tenant, subscription, resource group, provider namespace, resource type,
  location, SKU class, kind, and managedBy as source evidence.
- Keep tags only when they are within the configured evidence retention
  boundary; otherwise store tag keys and value fingerprints.
- Represent `changedBy`, managed identity principal IDs, client IDs, object
  IDs, and tenant IDs by class and deterministic fingerprint unless a later
  identity design explicitly admits raw IDs.
- Store changed property paths and truncation flags, not raw previous or new
  property values.
- Do not persist ARM deployment templates, Secret or Key Vault values, storage
  object contents, connection strings, access keys, tokens, private endpoint
  hostnames, public IP addresses, private IP addresses, log payloads, database
  contents, request bodies, or provider response bodies.
- ARM fallback payloads are separately schema-versioned under `armFallback`,
  byte-bounded before attachment, and then passed through the
  `azure_cloud_resource` redaction policy before persistence.

Metric labels and status keys must never include ARM IDs, subscription IDs,
resource group names, resource names, tags, identity GUIDs, DNS names, image
refs, KQL query text, URLs, or credential environment names.

## Reducer Contract

Azure facts stay provenance until reducers admit them:

1. `cloud_asset_resolution` resolves `azure_cloud_resource` facts into the
   shared `cloud_resource_uid` keyspace from ARM ID, tenant/scope identity,
   resource type, and location.
2. Drift and unmanaged-resource reducers compare declared IaC, Terraform state,
   and observed `azure_cloud_resource` facts. Provider observation alone does
   not decide ownership.
3. Tag reducers compare Azure tags without making tag value text a graph
   hot-path key.
4. Identity reducers treat `azure_identity_observation` as policy evidence only.
   Principal fingerprints do not become identity graph nodes unless a later
   identity design admits them.
5. Change reducers use `azure_resource_change` as freshness and investigation
   evidence attached to admitted inventory resources only. Delete changes remain
   tombstone candidates until inventory or reducer evidence confirms the final
   state.
6. Relationship reducers materialize graph edges only when both endpoints
   resolve exactly in the current allowed scope from observed
   `azure_cloud_resource` facts. Cross-tenant, cross-subscription, missing,
   unsupported, ambiguous, stale, invalid-type, self-loop, and partial-scope
   endpoints are counted and surfaced, never fabricated.
7. Image-reference reducers require digest-first or otherwise explicit
   tag-confidence behavior before using Azure image evidence in deployment or
   vulnerability paths.

Query output must preserve the source state: `exact`, `derived`, `partial`,
`stale`, `unavailable`, and `unsupported` have the same meaning as the shared
multi-cloud contract.

## Telemetry

Required runtime signals for implementation:

- claim start, success, failure, retry, heartbeat, and dead-letter counts
- Resource Graph and ARM calls by operation, scope kind, resource family,
  source lane, and status class
- throttle, quota remaining, quota reset, and backoff counters by operation and
  scope kind
- page count, `$skipToken` resume count, result truncation count, and
  partial-scope count
- facts emitted by fact kind, generation outcome, and scope kind
- permission-hidden, unsupported, stale, fallback-skipped, and redaction-warning
  counts
- freshness lag from change/read time to Eshu observed time
- reducer admitted, skipped, ambiguous, unresolved, partial, and unsupported
  counts

Use spans or structured logs for scoped diagnosis. Keep metric labels bounded
to enums such as operation, scope kind, resource family, source lane, fact kind,
status class, and outcome.

## Fixture Matrix

The first code PRs must prove these cases before any live smoke:

| Case | Required proof |
| --- | --- |
| Resource page replay | Re-emitting the same Resource Graph page converges to the same stable fact keys. |
| Pagination | `$skipToken` responses commit every page and checkpoint correctly. |
| Truncation | `resultTruncated` produces explicit warning or partial evidence. |
| Partial scope | Management-group or tenant query partial access emits `azure_collection_warning` and `partial` source state. |
| Permission-hidden subscription | Subscriptions without readable resources are visible as partial or unavailable evidence, not empty success. |
| Throttling | Quota headers drive retry/backoff and eventually emit retryable status or warning evidence. |
| Change delete | Delete change records do not fabricate a tombstone without inventory or reducer confirmation. |
| Actor redaction | `changedBy`, identity GUIDs, and client IDs are fingerprinted and raw change payloads are absent. |
| ARM fallback | Fallback only runs for allowlisted families, selects configured fields only, rejects oversized extension payloads, and emits separate warning evidence when skipped, throttled, timed out, or redacted. |
| Reducer truth | Exact, derived, partial, stale, unavailable, and unsupported Azure paths agree across reducer facts and API/MCP reads. |

## Implementation Order

1. Add fact constants, schema helpers, and fixture payload tests. **(done)**
2. Add a Resource Graph client adapter with mocked `Resources` and
   `resourcechanges` responses. **(live Resource Graph seam done; claimed-live
   command activation done, default-off; live smoke remains gated.)**
3. Add an allowlisted ARM fallback adapter with read-only mocked `GET`
   responses. **(done behind explicit injection; ARM-fallback live activation
   remains gated — claimed-live serves the `resource_graph` lane only.)**
4. Add the collector runtime and source fact emission. **(done: `azureruntime.Source`
   + `collector-azure-cloud` over a fixture/gated `PageProvider`, plus the
   claim-driven `-mode claimed-live` runtime through `collector.ClaimedService`
   with fixture-proven claim handoff; live smoke remains gated.)**
5. Add reducer admission for resource identity, tag evidence, change evidence,
   relationships, and warnings.
6. Add API/MCP readback truth tests for Azure evidence states.
7. Add Helm and live-smoke support only after the runtime and reducer contract
   pass fixture gates. **(default-off Helm exposure done — deployment, metrics
   service, ServiceMonitor, render-time validation, issue #3024; live smoke
   remains gated.)**

Remaining items gated until their own implementation PRs land with fixture and
live proof: live smoke proof against a real tenant, the hosted security posture
sign-off, ARM-fallback live activation, and later source-family scan loops.
