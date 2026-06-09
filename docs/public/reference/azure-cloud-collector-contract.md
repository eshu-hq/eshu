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

The first fixture-testable slice has landed in
`go/internal/collector/azurecloud`. It registers the Azure fact constants and
schema versions in `go/internal/facts/azure.go`, normalizes ARM resource
identity, redacts the provider extension payload, and emits
`azure_cloud_resource` and `azure_collection_warning` source facts from
fixture Resource Graph pages. It still performs **no live Azure calls**: the
`PageProvider` seam is fed by fixtures under
`go/internal/collector/azurecloud/testdata`.

Everything else stays gated. Do not add `eshu-collector-azure-cloud`, Helm
values, environment variables, collector-instance examples, `collector.Source`
runtime wiring, a scope `CollectorKind`, or query claims until implementation
PRs prove the live runtime, reducer path, additional fact kinds, telemetry in
the shared registry, and chart path. The remaining fact kinds
(`azure_cloud_relationship`, `azure_tag_observation`,
`azure_identity_observation`, `azure_resource_change`, `azure_dns_record`,
`azure_image_reference`) have registered constants but no envelope builders
yet.

The first implementation slice must be fixture-testable without live Azure
access. Live smoke tests are promotion proof, not the minimum proof for the
source contract.

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

1. Add fact constants, schema helpers, and fixture payload tests.
2. Add a Resource Graph client adapter with mocked `Resources` and
   `resourcechanges` responses.
3. Add an allowlisted ARM fallback adapter with read-only mocked `GET`
   responses.
4. Add the claim-driven collector runtime and source fact emission.
5. Add reducer admission for resource identity, tag evidence, change evidence,
   relationships, and warnings.
6. Add API/MCP readback truth tests for Azure evidence states.
7. Add Helm and live-smoke support only after the runtime and reducer contract
   pass fixture gates.

No-Observability-Change: the live Azure runtime stays gated. The first slice
adds fixture-driven fact emission and package-local bounded telemetry
instruments only. It adds no live runtime path, chart values, environment
variables, shared-registry telemetry series, or query claims.
