# GCP Cloud Collector Contract

This page defines the provider-specific design baseline for a future GCP cloud
collector. It is a child of the
[Multi-Cloud Runtime Collector Contract](multi-cloud-collector-contract.md) and
is not a deployed runtime promise.

The collector kind is `gcp`. It observes Google Cloud control-plane metadata
through Cloud Asset Inventory and emits source facts only. Reducers own
canonical `CloudResource` identity, drift, unmanaged-resource detection,
relationship graph writes, and API or MCP truth.

## Status

This contract is design-only. Do not add `eshu-collector-gcp-cloud`, Helm
values, environment variables, collector-instance examples, or query claims
until implementation PRs prove the runtime, fact schemas, reducer path,
fixtures, telemetry, and chart path.

The first implementation slice must be fixture-testable without live Google
Cloud access. Live smoke tests are promotion proof, not the minimum proof for
the source contract.

## Source Truth

Cloud Asset Inventory is the baseline source for GCP resource metadata,
policies, tags, relationships, and freshness hints. Scheduled full scans remain
authoritative for completeness. Pub/Sub feeds can wake the collector or target a
repair scan, but feeds do not replace full scans.

| Source lane | Use | Eshu boundary |
| --- | --- | --- |
| `assets.list` | Bounded inventory by organization, folder, or project parent. Use asset type, content type, read time, page size, and page token to create replayable shards. | Emits source facts for the current generation. It does not write graph truth. |
| `searchAllResources` | Discovery and targeted lookup by project, folder, or organization scope, including label, tag, location, state, KMS, and relationship query fields. | Useful for repair and bounded discovery. Do not store raw query strings in metrics or status. |
| Pub/Sub asset feeds | Freshness trigger for supported resource or policy changes. | The webhook/listener lane should enqueue normal collector work. The feed payload itself is not canonical graph truth. |
| Direct Google Cloud APIs | Optional gap fill for configured resource families that Cloud Asset Inventory cannot represent safely. | Must be allowlisted, bounded, read-only, and versioned separately in the source fact payload. |

Important provider constraints:

- Cloud Asset Inventory is eventually consistent. GCP update time, read time,
  and Eshu observed time must all stay visible.
- Relationship content can depend on Security Command Center Premium or
  Enterprise availability. Missing relationship support must produce explicit
  `unsupported` evidence, not a silent empty graph.
- Cloud Asset Inventory enforces per-project and per-organization quotas.
  Requests need bounded concurrency, retry, and backoff by method and parent
  scope.
- Feed creation and update have propagation delays and parent-level limits.
  Feeds are wakeups, not completeness proof.

## Scope And Generation

GCP collector claims should shard by configured parent scope, asset family,
content family, and location bucket:

```text
(collector_instance_id, parent_scope, asset_type_family, content_family, location_bucket)
```

| Field | Contract |
| --- | --- |
| `collector_kind` | Always `gcp`. |
| `collector_instance_id` | Configured runtime instance that owns target policy and credential environment. |
| `scope_id` | Stable Eshu scope for the parent scope and shard. Prefer `gcp:<parent_kind>:<parent_id>:<asset_family>:<content_family>:<location_bucket>`. |
| `parent_scope` | One of organization, folder, or project. Preserve both provider form and numeric or project ID where available. |
| `generation_id` | Collector or coordinator assigned for one bounded scan. |
| `stable_fact_key` | Deterministic key from fact kind, full resource name, asset type, content family, and provider update time where present. |
| `read_time` | Cloud Asset Inventory snapshot or response read time. |
| `update_time` | Provider update time on the asset or relationship when present. Missing update time must be explicit. |
| `page_checkpoint` | Page token or checkpoint metadata, never used as the durable resource identity. |

One claim per individual asset is only for repair or replay. Normal scans should
use bounded shards that can finish within the collector lease and quota budget.

## Fact Families

Initial GCP implementation should use provider-specific source facts, then let
reducers admit shared cloud identity. Do not introduce a catch-all source fact
unless the schema PR deliberately migrates AWS, GCP, and Azure together.

| Fact kind | Purpose | Required source fields |
| --- | --- | --- |
| `gcp_cloud_resource` | One Cloud Asset Inventory resource observation. | full resource name, asset type, parent scope, ancestors, location, read time, update time, state when present, labels, tag references, redaction policy version. |
| `gcp_tag_observation` | Direct and effective tag or label evidence. | full resource name, asset type, tag key/value fingerprints or bounded labels, source kind, inheritance state, read time. |
| `gcp_iam_policy_observation` | IAM policy evidence returned by Cloud Asset Inventory content types or IAM search. | full resource name, asset type, role, member class, member fingerprint, condition presence/fingerprint, etag fingerprint, read time. |
| `gcp_cloud_relationship` | Provider relationship evidence, including relationship type and related asset. | source full resource name, source asset type, relationship type, target full resource name, target asset type, read/update time, support state. |
| `gcp_dns_record` | DNS record metadata where Cloud Asset Inventory or safe service APIs expose it. | managed zone identity, record type, record name fingerprint, target fingerprints or bounded values, TTL, read time. |
| `gcp_image_reference` | Runtime image reference evidence from GKE, Cloud Run, Compute, or other safe resource metadata. | owning resource identity, image reference or digest when present, tag/digest confidence, container name fingerprint, read time. |
| `gcp_collection_warning` | Explicit partial, unsupported, stale, permission-hidden, quota, or redaction outcomes. | parent scope, asset family, content family, reason, outcome, retryability, read time. |

Facts must use `source_confidence=reported` for provider API data. Fixture facts
used in tests can use the same provider confidence because they model provider
responses, not local repository observations.

## Payload Boundaries

GCP payloads should preserve provider identity while avoiding private or
data-plane content:

- Keep full resource names in facts when needed for exact reducer joins.
- Keep project IDs, project numbers, folder numbers, organization numbers,
  asset types, locations, and ancestor chains as source evidence.
- Keep labels only when they are within the configured evidence retention
  boundary; otherwise store label keys and value fingerprints.
- Represent user, group, domain, and service-account members by class and
  deterministic fingerprint. Do not persist raw user emails or group emails.
- Do not persist raw IAM policy JSON, object contents, Secret payloads,
  startup scripts, environment variable values, database contents, logs,
  request bodies, public IP addresses, private IP addresses, or provider
  response bodies.
- Direct API fallback payloads need their own redaction policy version and
  fixture coverage.

Metric labels and status keys must never include full resource names, project
IDs, labels, tag values, IAM member identities, DNS names, image refs, URLs, or
credential environment names.

## Reducer Contract

GCP facts stay provenance until reducers admit them:

1. `cloud_asset_resolution` resolves `gcp_cloud_resource` facts into the shared
   `cloud_resource_uid` keyspace from full resource name, asset type, parent
   scope, and location.
2. Drift and unmanaged-resource reducers compare declared IaC, Terraform state,
   and observed `gcp_cloud_resource` facts. Provider observation alone does not
   decide ownership.
3. Tag reducers compare direct labels, direct tags, and effective inherited tags
   without making tag value text a graph hot-path key.
4. IAM reducers treat `gcp_iam_policy_observation` as policy evidence only.
   Principal fingerprints do not become user nodes unless a later identity
   design admits them.
5. Relationship reducers materialize graph edges only when both endpoints
   resolve exactly in the current allowed scope. Cross-project, cross-folder,
   missing, unsupported, ambiguous, and stale endpoints are counted and
   surfaced, never fabricated.
6. Image-reference reducers require digest-first or otherwise explicit
   tag-confidence behavior before using GCP image evidence in deployment or
   vulnerability paths.

Query output must preserve the source state: `exact`, `derived`, `partial`,
`stale`, `unavailable`, and `unsupported` have the same meaning as the shared
multi-cloud contract.

## Telemetry

Required runtime signals for implementation:

- claim start, success, failure, retry, heartbeat, and dead-letter counts
- API calls by method, parent scope kind, asset family, content family, and
  status class
- quota, throttle, and backoff counters by method and parent scope kind
- page count, page-token resume count, and page-token expiry count
- facts emitted by fact kind, generation outcome, and parent scope kind
- partial, unsupported, permission-hidden, stale, and redaction-warning counts
- freshness lag from provider update/read time to Eshu observed time
- reducer admitted, skipped, ambiguous, unresolved, and unsupported counts

Use spans or structured logs for scoped diagnosis. Keep metric labels bounded
to enums such as method, scope kind, asset family, content family, fact kind,
status class, and outcome.

## Fixture Matrix

The first code PRs must prove these cases before any live smoke:

| Case | Required proof |
| --- | --- |
| Resource page replay | Re-emitting the same `assets.list` page converges to the same stable fact keys. |
| Pagination | Multi-page responses commit every page and checkpoint correctly. |
| Stale generation | Older generations do not replace current facts or readiness. |
| Partial permission | Missing project, folder, or content-type permission emits `gcp_collection_warning` and `partial` source state. |
| Quota/backoff | Rate-limited responses retry within policy and eventually emit retryable status or warning evidence. |
| Unsupported relationship tier | Relationship content unavailable due to tier or type emits `unsupported`, not an empty success. |
| IAM redaction | User and group identities are fingerprinted, raw policy JSON is absent, and conditions are presence/fingerprint only. |
| Tag and label safety | Sensitive label values can be fingerprinted while exact configured labels remain bounded. |
| Direct API fallback | Fallback only runs for allowlisted families and emits separate warning evidence when skipped. |
| Reducer truth | Exact, derived, partial, stale, unavailable, and unsupported GCP paths agree across reducer facts and API/MCP reads. |

## Implementation Order

1. Add fact constants, schema helpers, and fixture payload tests.
2. Add a Cloud Asset Inventory client adapter with mocked `assets.list`,
   `searchAllResources`, and feed-trigger inputs.
3. Add the claim-driven collector runtime and source fact emission.
4. Add reducer admission for resource identity, tag evidence, relationships,
   and warnings.
5. Add API/MCP readback truth tests for GCP evidence states.
6. Add Helm and live-smoke support only after the runtime and reducer contract
   pass fixture gates.

No-Observability-Change: this page documents a gated design contract only. It
does not add runtime code, chart values, fact schemas, or telemetry series.
