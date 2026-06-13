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

The first fixture-testable slice is implemented: the `gcp_cloud_resource`,
`gcp_cloud_relationship`, label-backed `gcp_tag_observation`,
`gcp_iam_policy_observation`, `gcp_dns_record`, `gcp_image_reference`, and
`gcp_collection_warning` source fact kinds
(`go/internal/facts/gcp.go`), the Cloud Asset Inventory parser, identity
normalizer, redaction policy, envelope builders, generation accumulator with
fencing, and scoped telemetry instruments (`go/internal/collector/gcpcloud`).
This slice is fixture-driven and makes no live Google Cloud calls.

The second slice adds fixture-driven runtime scaffolding: a `collector.Source`
implementation (`go/internal/collector/gcpcloud/gcpruntime`) that drains Cloud
Asset Inventory pages through a `PageProvider` seam, fences generations, emits
the scoped telemetry, and a `cmd/collector-gcp-cloud` binary that wires the
source and a status-recording committer from a declarative offline config. The
live Cloud Asset Inventory transport is the documented, unimplemented,
unwired `gcpruntime.LiveClient` seam; no code or test makes a live Google Cloud
call.

Shared multi-cloud reducer admission and API/MCP readback for the
`gcp_cloud_resource` identity are now implemented and fixture-proven. The
additive `cloud_inventory_admission` reducer domain
(`go/internal/reducer/cloud_inventory_admission.go`, wired in
`go/cmd/reducer`) admits `gcp_cloud_resource` into the shared `CloudResource`
identity keyspace (`cloud_resource_uid`) as the reducer-owned
`reducer_cloud_resource_identity` read model, using deterministic provider
identity resolution (`go/internal/correlation/cloudinventory`),
declared-over-applied-over-observed management-origin precedence,
ambiguous/unsupported/unresolved accounting, and stale-generation supersession.
The `GET /api/v0/cloud/inventory` handler
(`go/internal/query/cloud_inventory_readback.go`) and the
`list_cloud_resource_inventory` MCP tool return bounded, truth-labeled GCP
identity rows (exact, semantic-facts basis) without leaking raw provider
locators.

The rest of this contract remains gated. Do not add Helm values, environment
variables, chart claims, or a live Cloud Asset Inventory transport until later
implementation PRs prove the live runtime adapter (`gcpruntime.LiveClient`) and
chart path. The relationship, tag, IAM, DNS, and image-reference fact kinds and
schema versions are registered in `go/internal/facts/gcp.go`, and **all five
envelope builders are implemented and unit-proven**:
`NewCloudRelationshipEnvelope` (provenance-only: both endpoint full resource
names, asset types, relationship type, bounded support state; resolves no
endpoints, writes no graph edge); `NewTagObservationEnvelope` (tag keys kept,
every tag value fingerprinted); `NewIAMPolicyObservationEnvelope` (members
fingerprinted by class via `FingerprintMember`, role + condition presence kept,
no raw policy JSON); `NewDNSRecordEnvelope` (record name and targets
fingerprinted); and `NewImageReferenceEnvelope` (digest-first confidence,
fingerprinted container name). The generation accumulator now emits
`gcp_cloud_relationship` from parsed CAI `relatedAsset` evidence with source and
target full resource names preserved as provenance-only evidence, emits
`gcp_tag_observation` from parsed CAI resource labels when a redaction key is
configured, with `source_kind=label`, and emits
`gcp_iam_policy_observation` from parsed CAI IAM bindings when members are
usable. It also emits `gcp_dns_record` from parsed CAI
`dns.googleapis.com/ResourceRecordSet` assets when record type, record name, and
managed-zone identity are usable. Direct/effective GCP tag API collection,
fact-kind-specific reducer handling, and any GCP graph projection remain
follow-up work under #1997.

The implemented slices stay fixture-testable without live Google Cloud access.
Live smoke tests are promotion proof, not the minimum proof for the source
contract.

No-Regression Evidence: `go test ./internal/reducer ./internal/query
./internal/mcp -run CloudInventory -count=1` and `go test
./internal/storage/postgres -run CloudInventory -count=1` prove `gcp_cloud_resource`
admission into the shared `cloud_resource_uid` keyspace, management-origin
precedence, ambiguous/unsupported/unresolved accounting, stale-generation
supersession, and bounded truth-labeled readback that never leaks raw provider
locators. `go test ./internal/collector/gcpcloud -run
'TestGeneration|TestNewTagObservationEnvelope' -count=1` proves label-backed tag
observation emission is deduplicated with generation output, skips unlabeled
resources, and fingerprints raw label values. `go test
./internal/collector/gcpcloud -run 'TestGenerationBuildEmitsIAMPolicyObservations|TestNewIAMPolicyObservationEnvelope' -count=1`
proves IAM bindings emit redacted policy observations without raw member
identities, raw etags, raw condition text, or same-role conditional key
collisions. `go test ./internal/collector/gcpcloud -run
'TestGenerationBuildEmitsDNSRecordObservations|TestNewDNSRecordEnvelope' -count=1`
proves DNS record assets emit redacted record observations without raw DNS names
or targets in fact payloads or source refs. `go test
./internal/collector/gcpcloud ./internal/collector/gcpcloud/gcpruntime -run
'TestParseAssetsListPageRelationships|TestGenerationBuildEmitsRelationshipObservationsForRelatedAssets|TestSourceEmitsRelationshipFactsFromFixturePage'
-count=1` proves CAI `relatedAsset` evidence emits one
`gcp_cloud_relationship` fact with source and target full resource names, bounded
relationship type, support state, read time, and fact-kind telemetry.
`go test ./internal/collector/gcpcloud ./internal/collector/gcpcloud/gcpruntime
-run 'TestParseAssetsListPageImageReferences|TestGenerationBuildEmitsImageReferenceObservationsForRuntimeContainers|TestGenerationBuildSkipsImageReferenceObservationsWithoutRedactionKey|TestSourceEmitsImageReferenceFactsFromFixturePage'
-count=1` proves Cloud Run service/job image metadata emits
`gcp_image_reference` facts with digest-vs-tag confidence, read time, and
fact-kind telemetry while skipping zero-key generation emission and dropping raw
runtime template/env blobs.

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
- Keep runtime image references and digests only from bounded resource metadata.
  Fingerprint container names; do not persist raw runtime template blobs or
  environment variable names/values.
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
| DNS redaction | Record names and targets are fingerprinted, and no raw DNS names reach facts, source refs, metrics, or status. |
| Image-reference redaction | Cloud Run service/job image metadata emits image-reference facts, container names are fingerprinted, and raw runtime template/env blobs are dropped. |
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

Observability change: the first slice adds the `gcp_cloud_resource`,
`gcp_cloud_relationship`, `gcp_tag_observation`,
`gcp_iam_policy_observation`, `gcp_dns_record`, `gcp_image_reference`, and
`gcp_collection_warning` fact schemas and the scoped GCP collector telemetry
series listed under
[Telemetry](#telemetry)
(`eshu_dp_gcp_cloud_*`). It does not add chart values, environment variables, or
a runtime binary; those remain deferred.

## Runtime Scaffolding Evidence

This evidence covers the second slice: the `gcpruntime` source and the
`collector-gcp-cloud` binary. These are hot-path collector files, so they carry
tracked performance and observability evidence here.

Collector Performance Evidence: see the No-Regression Evidence block below.

Collector Observability Evidence: see the Observability Evidence block below.

Collector Deployment Evidence: this slice adds no Helm chart values, no
ServiceMonitor, and no environment-variable contract. The binary is
fixture-driven scaffolding launched with `-config` and `-redaction-key-file`
file paths only. Deployment, chart, and ServiceMonitor wiring are explicitly
deferred to a later slice once the reducer path exists, per the Status section
above. No deployment surface changes in this PR.

No-Observability-Change: this slice uses the existing scoped GCP collector
instruments registered in the first slice
(`eshu_dp_gcp_cloud_claims_total`, `eshu_dp_gcp_cloud_pages_total`,
`eshu_dp_gcp_cloud_page_token_resumes_total`,
`eshu_dp_gcp_cloud_facts_emitted_total`, `eshu_dp_gcp_cloud_warnings_total`,
`eshu_dp_gcp_cloud_freshness_lag_seconds`). Tag, IAM, and DNS emission record
through the existing `fact_kind` dimension with `gcp_tag_observation`,
`gcp_iam_policy_observation`, and `gcp_dns_record`; relationship emission uses
the same existing dimension with `gcp_cloud_relationship`; image-reference
emission uses the same existing dimension with `gcp_image_reference`. It
registers no new metric series, adds no new label keys, and changes no telemetry
contract.

No-Regression Evidence:

- Baseline: before this slice there was no GCP collector runtime; the source
  path did not exist, so the comparison is against the empty/no-runtime
  baseline and against the AWS collector source shape this mirrors.
- After: `go test ./internal/collector/gcpcloud/gcpruntime/...
  ./cmd/collector-gcp-cloud/... ./internal/collector/gcpcloud/...
  ./internal/facts/... -count=1` passes.
- Backend/version: no graph or database backend is exercised. The source builds
  facts in memory through the fixture `PageProvider`; the binary commits through
  the existing `postgres.IngestionStore` unchanged by this slice.
- Input shape: two-page `assets.list` fixtures (three resources) per scope, plus
  stale-generation, dangling-page-token, multi-scope, and empty-scope cases.
- Terminal counts: one `CollectedGeneration` per configured scope; three
  `gcp_cloud_resource` facts and two label-backed `gcp_tag_observation` facts
  for the two-page resource fixture; the IAM-policy fixture emits one
  `gcp_cloud_resource` fact and two `gcp_iam_policy_observation` facts; the DNS
  fixture emits one `gcp_cloud_resource` fact and one `gcp_dns_record` fact; the
  relationship fixture emits one `gcp_cloud_resource` fact and one
  `gcp_cloud_relationship` fact; the image-reference fixture emits two
  `gcp_cloud_resource` facts and three `gcp_image_reference` facts; one
  `gcp_collection_warning` for the stale and page-token-expired cases; zero
  facts emitted for a fenced-out stale generation beyond its single warning.
- Telemetry/log/status evidence: see the observability evidence below.
- Why safe: the source is single-goroutine per `collector.Service`, reuses the
  existing fixture-tested `Generation`/`GenerationTracker` dedupe and fencing,
  performs no live call, and adds no new database query, Cypher, lease, or
  concurrent writer. Pagination resumes strictly by continuation token, so an
  expired token degrades to a durable partial warning rather than truncation.

Observability Evidence:

- The source records the existing scoped `eshu_dp_gcp_cloud_*` instruments
  (claims, pages, page-token resumes, facts emitted, warnings, freshness lag)
  through `gcpcloud.Metrics`. Every metric label is a bounded enum: collector
  kind, claim status, parent scope kind, fact kind, warning kind, and outcome.
- The status committer records `eshu_dp_gcp_cloud_claims_total` with
  `status=succeeded` or `status=failed` on commit outcome.
- One structured log line per committed scope reports bounded counts only
  (page, resource, and warning counts plus the scope id and bounded families).
  No instrument or log field carries a resource name, project id, label value,
  IAM member, DNS name, image reference, URL, or credential name. Credentials
  are referenced by name only and the redaction key is never logged.
