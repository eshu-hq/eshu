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

It still performs **no live Azure calls**. The live Resource Graph/ARM client is
a documented seam (`azureruntime.LiveProviderFactory`) that is inert and returns
`ErrLiveProviderGated`; it is never the default. Tests and the binary's offline
mode use a fixture or file-backed `PageProvider`.

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

Everything else stays gated. Do not add Helm values, chart paths, claim-driven
workflow scheduling, or a live Resource Graph/ARM transport until implementation
PRs prove the live runtime adapter (`azureruntime.LiveProviderFactory`) and
chart path. The `azure_cloud_relationship` envelope builder
(`NewRelationshipEnvelope`) is implemented and unit-proven as provenance-only
(both endpoint ARM identities, relationship type, and a bounded support state;
it resolves no endpoints and writes no graph edge). The
`azure_identity_observation` envelope builder (`NewIdentityObservationEnvelope`)
is implemented and unit-proven: it fingerprints every
principal/client/object/tenant GUID with the redaction key (raw GUIDs never
persist) and carries the bounded identity type, role class, and assignment scope
as policy evidence only. The remaining fact kinds (`azure_resource_change`,
`azure_dns_record`,
`azure_image_reference`) have registered constants but no envelope builders yet;
their fact-kind-specific reducer handling follows once those builders exist.

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
readback without exposing tag value text.

No-Observability-Change: the live Azure runtime adapter stays gated. The
runtime scaffolding slice adds a non-claimed source and binary that emit a
bounded per-target span (`collector.azure.scope_scan`) and reuse the
package-local bounded-label Azure instruments only. It adds no live Azure call,
no chart values, no shared-registry telemetry series, and no query claims.

## Source Truth

Azure Resource Graph is the baseline source for broad inventory across
subscriptions, management groups, or tenant-level scopes. Scheduled Resource
Graph inventory scans remain authoritative for completeness. Resource Graph
change data is freshness input, not a replacement for full scans.

| Source lane | Use | Eshu boundary |
| --- | --- | --- |
| Resource Graph `Resources` API | Bounded inventory and metadata queries over configured subscriptions or management groups. Use explicit KQL, `$top`, `$skipToken`, `resultTruncated`, and scope metadata. | Emits source facts for the current generation. It does not write graph truth. |
| Resource Graph tables and joins | Safe discovery for related resource classes when the table supports the relationship. | Provider relationship evidence only. Reducers resolve both endpoints before graph writes. |
| Resource Graph `resourcechanges` | Freshness hint for create, update, and delete records with change type, target resource, operation, client type, and timestamp. | Emits change evidence. It does not prove final resource state and cannot replace inventory scans. |
| Azure Resource Manager fallback | Optional read-only `GET` calls for configured resource families where Resource Graph lacks safe detail. | Must be allowlisted, bounded, read-only, and versioned separately in the source fact payload. |

Important provider constraints:

- Resource Graph returns only resources the principal can read. Partial
  subscription or management-group access must be explicit.
- Resource Graph queries are KQL. Query strings are implementation metadata and
  must not appear in metric labels or user-facing status.
- Resource Graph throttles queries and exposes quota reset information in
  response headers. Requests need bounded concurrency, retry, and backoff.
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
- ARM fallback payloads need their own redaction policy version and fixture
  coverage.

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
   evidence. Delete changes are tombstone candidates only after inventory or
   reducer evidence confirms the final state.
6. Relationship reducers materialize graph edges only when both endpoints
   resolve exactly in the current allowed scope. Cross-tenant,
   cross-subscription, missing, unsupported, ambiguous, stale, and partial-scope
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
| ARM fallback | Fallback only runs for allowlisted families and emits separate warning evidence when skipped. |
| Reducer truth | Exact, derived, partial, stale, unavailable, and unsupported Azure paths agree across reducer facts and API/MCP reads. |

## Implementation Order

1. Add fact constants, schema helpers, and fixture payload tests. **(done)**
2. Add a Resource Graph client adapter with mocked `Resources` and
   `resourcechanges` responses.
3. Add an allowlisted ARM fallback adapter with read-only mocked `GET`
   responses.
4. Add the collector runtime and source fact emission. **(runtime scaffolding
   done: `azureruntime.Source` + `collector-azure-cloud` over a fixture/gated
   `PageProvider`; live adapter and claim-driven scheduling remain gated.)**
5. Add reducer admission for resource identity, tag evidence, change evidence,
   relationships, and warnings.
6. Add API/MCP readback truth tests for Azure evidence states.
7. Add Helm and live-smoke support only after the runtime and reducer contract
   pass fixture gates.

The remaining live Azure runtime adapter, reducer admission, additional fact
families, API/MCP readback, and chart wiring stay gated until their own
implementation PRs land with fixture and live proof.
