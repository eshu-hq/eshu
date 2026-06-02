# Multi-Cloud Runtime Collector Contract

This page defines the shared contract for cloud runtime collectors beyond AWS.
It is a design baseline for GCP and Azure work, not a deployed runtime promise.

The boundary stays facts-first: cloud collectors observe provider control-plane
truth and emit typed facts. Reducers own canonical `CloudResource` identity,
drift, unmanaged-resource detection, cross-source correlation, graph writes, and
API or MCP truth.

## Status

GCP and Azure collectors are gated source families. Do not add Helm values,
runtime commands, environment variables, or API claims for them until their
collector runtime, fact contract, reducer contract, fixtures, telemetry, and
chart path are implemented.

Provider work should remain separate:

| Provider | Collector kind | Source contract | Implementation issue |
| --- | --- | --- | --- |
| AWS | `aws` | Implemented claim-driven AWS control-plane metadata collection. | Existing AWS collector work. |
| GCP | `gcp` | Design-only [Cloud Asset Inventory collector](gcp-cloud-collector-contract.md). | [#21](https://github.com/eshu-hq/eshu/issues/21) |
| Azure | `azure` | Design-only [Azure Resource Graph and ARM collector](azure-cloud-collector-contract.md). | [#22](https://github.com/eshu-hq/eshu/issues/22) |

Do not hide provider differences behind one generic cloud collector. GCP and
Azure have different hierarchy, identity, permission, pagination, freshness,
quota, IAM, and relationship semantics.

## IaC First

Source-controlled IaC, GitOps, Terraform source, and Terraform state stay the
preferred evidence when they are current. They explain intended and applied
configuration without spending provider quota, and they usually carry the code
path operators need to change.

Direct provider collection is still required for:

- environments with no IaC or incomplete IaC coverage
- unmanaged or manually created resources
- runtime drift between declared, applied, and observed state
- freshness validation after provider change notifications
- provider-only metadata that safe IaC parsers cannot see

Reducers and read surfaces must preserve the evidence layer. Declared,
applied, and observed provider evidence are different inputs; provider
observations do not overwrite declared IaC truth by themselves.

## Source Truth

GCP baseline:

- Use Cloud Asset Inventory for scheduled full scans across organization,
  folder, or project scopes.
- Use Cloud Asset Inventory `assets.list` and `searchAllResources` for bounded
  inventory and metadata reads.
- Use Cloud Asset Inventory Pub/Sub feeds as freshness triggers, not as the
  only completeness path.
- Use direct Google Cloud service APIs only for configured gaps that Cloud Asset
  Inventory cannot represent safely.

Azure baseline:

- Use Azure Resource Graph for scheduled inventory queries across subscriptions
  or management groups.
- Use Resource Graph change data as freshness hints and targeted rescan input,
  not as the only completeness path.
- Use Azure Resource Manager provider APIs only for configured resource
  families where Resource Graph does not expose enough safe metadata.
- Treat partial subscription or management-group access as explicit evidence
  state, not as success.

Official source contracts checked for this baseline:

- [Cloud Asset Inventory overview](https://cloud.google.com/asset-inventory/docs/asset-inventory-overview)
- [Cloud Asset Inventory `assets.list`](https://cloud.google.com/asset-inventory/docs/reference/rest/v1/assets/list)
- [Cloud Asset Inventory `searchAllResources`](https://cloud.google.com/asset-inventory/docs/reference/rest/v1/TopLevel/searchAllResources)
- [Cloud Asset Inventory Pub/Sub feeds](https://cloud.google.com/asset-inventory/docs/monitor-asset-changes)
- [Azure Resource Graph overview](https://learn.microsoft.com/en-us/azure/governance/resource-graph/overview)
- [Azure Resource Graph Resources API](https://learn.microsoft.com/en-us/rest/api/azureresourcegraph/resourcegraph/resources/resources)

## Scope And Generation

Every cloud collector claim needs a durable source scope and generation:

| Field | Contract |
| --- | --- |
| `collector_kind` | Provider collector kind, such as `aws`, `gcp`, or `azure`. |
| `collector_instance_id` | Configured instance that owns credentials and target policy. |
| `scope_id` | Durable source-local scope. Use account/region/service for AWS, organization/folder/project for GCP, and tenant/subscription or management-group scope for Azure. |
| `generation_id` | Collector or coordinator assigned observation generation for one bounded scan. |
| `stable_fact_key` | Idempotency key inside the scope and generation. Duplicate delivery must converge. |
| `source_ref` | Provider source record reference with bounded URI, source record ID, read time, update time, page token, or checkpoint metadata. |
| `source_confidence` | `reported` for provider API data unless the collector is reading an owned local artifact. |
| `freshness` | Provider update/read time plus Eshu observation time. Missing provider update time must be explicit. |

Avoid one claim per individual resource except for repair or replay. Prefer
bounded shards by provider, parent scope, resource type family, and location
bucket.

## Provider Identity

Collectors must preserve raw provider identity and add normalized fields. Raw
identity is source evidence; reducer output decides canonical identity.

| Provider | Raw identity to preserve | Normalized fields |
| --- | --- | --- |
| AWS | ARN or provider-native ID where the service has no ARN. | account, partition, region, service, resource type, resource ID. |
| GCP | Cloud Asset Inventory full resource name. | asset type, project ID/number, folder number, organization number, ancestors, location. |
| Azure | ARM resource ID. | tenant ID, subscription ID, resource group, provider namespace, resource type, resource name, location. |

Provider-specific extension payloads must be versioned and redacted. They may
carry safe control-plane metadata, but they must not carry object contents,
secret values, private payloads, or data-plane records.

## Fact Family Shape

Initial GCP and Azure implementation should use provider-specific source fact
kinds, then let reducers admit shared cloud identity. Do not introduce one
catch-all `cloud_resource` source fact unless the schema PR deliberately
migrates AWS, GCP, and Azure together.

Provider fact families should cover:

| Source evidence | GCP example | Azure example | Reducer ownership |
| --- | --- | --- | --- |
| Resource inventory | `gcp_cloud_resource` | `azure_cloud_resource` | Canonical `CloudResource` identity and graph nodes. |
| Relationship evidence | `gcp_cloud_relationship` | `azure_cloud_relationship` | Canonical graph edges after endpoint resolution. |
| Tags and labels | `gcp_tag_observation` | `azure_tag_observation` | Shared tag taxonomy and source precedence. |
| Identity or policy metadata | `gcp_iam_policy_observation` | `azure_identity_observation` | IAM/security graph projection after provider-specific gates. |
| DNS metadata | `gcp_dns_record` | `azure_dns_record` | DNS graph or read-model projection when supported. |
| Image/runtime references | `gcp_image_reference` | `azure_image_reference` | Image identity and deployment correlation. |
| Coverage warnings | `gcp_collection_warning` | `azure_collection_warning` | Status, readiness, and operator diagnostics. |

Each payload must include provider, raw identity, normalized identity, source
timestamps, redaction policy version, and a provider-specific extension object
with an explicit schema version.

## Reducer Contract

The shared reducer path owns:

1. Load source facts for the current generation.
2. Resolve provider identity into the shared `cloud_resource_uid` keyspace.
3. Compare declared IaC, applied Terraform state, and observed provider facts.
4. Publish drift, unmanaged-resource, and coverage decisions as reducer-owned
   facts or read-model rows.
5. Project canonical graph nodes and edges only when endpoint resolution is
   exact enough for the graph contract.
6. Publish phase/status evidence so workflow completeness, API, and MCP reads
   agree.

Provider relationship records are provenance until the reducer resolves both
endpoints. Cross-account, cross-project, cross-subscription, missing,
unsupported, or ambiguous targets are counted and surfaced; they must not
produce fabricated or dangling graph edges.

## Query Truth

Cloud evidence reads must distinguish source state from canonical truth. The
top-level response still follows the [Truth Label Protocol](truth-label-protocol.md),
while per-item or per-path evidence should use these states:

| State | Meaning |
| --- | --- |
| `exact` | Provider evidence and reducer-owned canonical identity agree for one path. |
| `derived` | Deterministic correlation exists, but it is not direct provider proof. |
| `partial` | The collector could read only part of the configured scope or content family. |
| `stale` | The latest accepted generation is older than the configured freshness window. |
| `unavailable` | The provider source was configured but unreachable, unauthorized, or rate-limited without current evidence. |
| `unsupported` | The provider, tier, API, resource family, or relationship type does not expose this evidence. |

Reads must not convert `partial`, `stale`, `unavailable`, or `unsupported`
states into silent fallback truth.

## Telemetry

Runtime signals need to let an operator answer whether collection is complete,
fresh, throttled, partial, or stuck:

- claims started, completed, failed, retried, and dead-lettered
- API calls by provider, method or operation, scope kind, and status class
- throttle, quota, and backoff events
- page counts and continuation-token or skip-token resumes
- partial-scope, permission-hidden, and unsupported-content counts
- facts emitted by provider, collector kind, fact kind, and generation outcome
- freshness lag from provider update/read time to Eshu observed time
- reducer phase counts for admitted, skipped, ambiguous, and unresolved records

Metric labels may include bounded enums such as provider, collector kind,
method, status class, scope kind, fact kind, and outcome. They must not include
resource IDs, names, project IDs, subscription IDs, URLs, tags, labels, policy
bodies, query text, or credential names.

## Testing Matrix

Provider implementation PRs need fixture-first tests for:

- pagination and continuation-token resume
- idempotent re-emission of the same generation
- stale generation rejection
- quota and throttle backoff
- partial permission, hidden resource, and missing subscription/project cases
- unsupported relationship or content type cases
- redaction of raw provider payload fields
- provider API fallback bounds
- reducer drain, graph endpoint resolution, and skip accounting
- API and MCP truth labels for exact, derived, partial, stale, unavailable, and
  unsupported evidence

Live smoke tests can prove credentials and provider behavior later, but the
first implementation slice must be unit and fixture-testable without live cloud
access.

No-Observability-Change: this page documents a gated design contract only. It
does not add runtime code, chart values, fact schemas, or telemetry series.
