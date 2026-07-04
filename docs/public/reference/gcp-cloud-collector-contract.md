# GCP Cloud Collector Contract

This page defines the provider-specific design baseline for the GCP cloud
collector. It is a child of the
[Multi-Cloud Runtime Collector Contract](multi-cloud-collector-contract.md) and
does not promise sanitized live Google Cloud target proof until the smoke gate is
proven.

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
command also has explicit claimed-live mode: it selects one enabled
claim-capable GCP collector instance, requires `live_collection_enabled=true`,
uses workflow claims for generation/fencing identity, and runs through
`collector.ClaimedService`. The live Cloud Asset Inventory transport is the
explicit-injection `gcpruntime.LiveClient` REST `PageProvider` for
`assets.list`; it requires read-only caller-supplied credentials, bounded page
size, response bytes, timeouts, retries, backoff, OAuth scope, and asset-family
filters. The Helm chart starts only explicit claimed-live mode and remains
default-off. No test makes a live Google Cloud call.

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

Label-backed `gcp_tag_observation` evidence is now admitted through the same
shared cloud inventory tag-evidence loader as Azure tags. The loader reads only
one sealed `(scope_id, generation_id)`, maps the GCP full resource name into
the `cloud_resource_uid` keyspace, and attaches keyed tag-value fingerprints to
an already-admitted canonical resource. Tag evidence never admits a resource on
its own, and cloud inventory readback surfaces only `tag_value_fingerprints`,
not raw tag value text.

`gcp_image_reference` evidence now feeds the existing
`container_image_identity` reducer. The evidence pass keeps GCP image references
digest-first: image facts with an explicit digest are converted to immutable
digest references, tag-only facts require existing OCI registry tag evidence,
and ambiguous or unresolved tag outcomes stay diagnostic reducer decisions. The
GCP owning resource full name remains source evidence only; it does not mint
repository, workload, service, or graph truth.

The GCP IAM secrets/IAM mirror now includes service-account impersonation trust
evidence. Cloud Asset Inventory ServiceAccount resources retain
`resource.data.email` only long enough to compute the target service-account
member fingerprint and email digest. IAM bindings on those ServiceAccount
resources that grant `roles/iam.serviceAccountTokenCreator`,
`roles/iam.serviceAccountUser`, or `roles/iam.workloadIdentityUser` emit
`gcp_iam_trust_policy` facts; the Workload Identity role also records the
redaction-safe workload-pool subject fingerprint used by Kubernetes
`k8s_gcp_workload_identity_binding` facts. Raw service-account email, workload
pool, namespace, Kubernetes ServiceAccount name, and IAM member strings are not
persisted in those trust facts.

The rest of this contract remains gated. Do not claim sanitized target smoke
until later implementation PRs prove the live target path. The
relationship, tag, IAM, DNS, and image-reference fact kinds and schema versions
are registered in `go/internal/facts/gcp.go`, and **all five
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
managed-zone identity are usable. Opt-in direct/effective Resource Manager tag
API collection emits tag-key/value-fingerprint evidence and inheritance state;
sanitized live target smoke remains follow-up work under #1997. Raw
`gcp_iam_policy_observation`,
`gcp_dns_record`, and `gcp_collection_warning` facts are intentionally
provenance-only or audit evidence until separate reducer/read-model contracts
admit them.

Command Runtime Evidence: `go test ./cmd/collector-gcp-cloud ./internal/collector/gcpcloud/... -count=1` proves fixture mode remains available, claimed-live mode is explicit, live collection is gated, workflow claims supply generation/fencing identity, and tests build the `gcpruntime.LiveClient` runner without live Google Cloud credentials.

Chart Deployment Evidence: `go test ./internal/runtime -run 'TestHelmGCPCloudCollectorDeployment|TestGCPCloudCollectorBinaryIsBuiltInstalledAndDocumented' -count=1` proves GCP Helm exposure is default-off, starts `-mode claimed-live`, mounts redaction material from a read-only Secret file, renders metrics Service, ServiceMonitor, NetworkPolicy, and PodDisruptionBudget coverage, and builds the release/local binary entry.

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
`go test ./internal/relationships -run GCP -count=1` proves the cross-repo
resolver consumes only supported GCP relationship facts whose source and target
resource names uniquely match distinct catalog repositories, and skips one-sided,
ambiguous, self, unsupported, IAM, DNS, and image-reference inputs.
`go test ./internal/storage/postgres -run
'ListLatestRelationshipFactRecordsQuery.*GCP|QualifiesFactColumns' -count=1`
and `go test ./internal/reducer -run
'GCPCloudRelationship|EvidenceTypeMapsGCP' -count=1` prove deferred
relationship backfill includes `gcp_cloud_relationship` facts and resolved
relationship artifacts keep the stable `gcp_cloud_relationship` evidence type.
`go test ./internal/collector/gcpcloud ./internal/collector/gcpcloud/gcpruntime
-run 'TestParseAssetsListPageImageReferences|TestGenerationBuildEmitsImageReferenceObservationsForRuntimeContainers|TestGenerationBuildSkipsImageReferenceObservationsWithoutRedactionKey|TestSourceEmitsImageReferenceFactsFromFixturePage'
-count=1` proves Cloud Run service/job image metadata emits
`gcp_image_reference` facts with digest-vs-tag confidence, read time, and
fact-kind telemetry while skipping zero-key generation emission and dropping raw
runtime template/env blobs. `go test
./internal/storage/postgres -run 'Test(PostgresCloudTagEvidenceLoaderMaps.*|CloudTagEvidenceQueryIncludesGCPTagFacts)' -count=1`
proves the shared tag-evidence loader accepts GCP tag facts, reads
`full_resource_name` as the bounded source identity, and keeps the SQL allowlist
in lockstep with the Go mapping. `go test ./internal/reducer -run
'Test(ContainerImageIdentityFactKindsIncludesGCPImageReferences|BuildContainerImageIdentityDecisions(ConsumesGCPDigestReference|ResolvesGCPTagOnlyWithRegistryEvidence))'
-count=1` proves `gcp_image_reference` facts are loaded by the
`container_image_identity` domain, explicit GCP digests become exact
digest-keyed identity decisions, tag-only GCP facts require OCI registry tag
evidence, and the GCP owning resource identity does not invent repository
anchors. `go test ./internal/storage/postgres -run
TestFactStoreListActiveContainerImageIdentityFactsUsesActiveIdentityGenerations
-count=1` proves the active cross-scope image-identity fact loader includes GCP
image-reference facts while preserving active-generation and tombstone
predicates. `go test ./internal/projector -run
TestBuildProjectionQueuesContainerImageIdentityForGCPImageReference -count=1`
proves GCP image-reference generations enqueue the existing
`container_image_identity` reducer domain.

No-Observability-Change: GCP tag admission reuses the existing
`cloud_inventory_admission` reducer execution, the existing
`PostgresCloudTagEvidenceLoader` read path, existing `postgres.query` spans and
query-duration metrics, and the existing cloud inventory readback field
`tag_value_fingerprints`. It adds no route, table, queue domain, worker,
runtime knob, metric name, metric label, span name, graph write, or chart
surface. GCP image identity admission reuses the existing
projector reducer-intent handoff, `container_image_identity` reducer execution,
durable `reducer_container_image_identity` fact writer, active OCI registry
evidence loader, active-generation fact predicates, and
`eshu_dp_container_image_identity_decisions_total` outcome counter; it adds no
live provider call, route, queue domain, worker, runtime knob, metric label,
graph write, or chart surface. GCP relationship resolver evidence reuses the
existing relationship evidence backfill, resolver admission, and cross-repo
resolution telemetry. It adds no graph write, route, queue domain, worker,
runtime knob, metric name, metric label, span name, or chart surface.

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
| `gcp_cloud_resource` | One Cloud Asset Inventory resource observation. | full resource name, asset type, parent scope, ancestors, location, read time, update time, state when present, labels, tag references, redaction policy version, bounded typed-depth `attributes` map (schema 1.1.0+), `correlation_anchors`. |
| `gcp_tag_observation` | Direct and effective tag or label evidence. | full resource name, asset type, tag key/value fingerprints or bounded labels, source kind, inheritance state, read time. |
| `gcp_iam_policy_observation` | IAM policy evidence returned by Cloud Asset Inventory content types or IAM search. | full resource name, asset type, role, member class, member fingerprint, condition presence/fingerprint, etag fingerprint, read time. |
| `gcp_iam_principal` | Secrets/IAM mirror principal evidence for GCP service-account grantees. | principal fingerprint, principal class, provider, read time, source role/resource anchors. |
| `gcp_iam_trust_policy` | ServiceAccount impersonation trust evidence derived from IAM bindings on `iam.googleapis.com/ServiceAccount` resources. | target service-account principal fingerprint, target email digest, target cloud-resource uid, trusted member fingerprint/class, impersonation role/mode, optional Workload Identity subject fingerprint, condition presence/fingerprint, read time. |
| `gcp_iam_permission_policy` | Secrets/IAM mirror permission evidence for GCP service-account grants. | principal fingerprint, role, resource uid/anchor, provider, read time, bounded capability classification. |
| `gcp_cloud_relationship` | Provider relationship evidence, including relationship type and related asset. | source full resource name, source asset type, relationship type, target full resource name, target asset type, read/update time, support state. |
| `gcp_dns_record` | DNS record metadata where Cloud Asset Inventory or safe service APIs expose it. | managed zone identity, record type, record name fingerprint, target fingerprints or bounded values, TTL, read time. |
| `gcp_image_reference` | Runtime image reference evidence from GKE, Cloud Run, Compute, or other safe resource metadata. | owning resource identity, image reference or digest when present, tag/digest confidence, container name fingerprint, read time. |
| `gcp_collection_warning` | Explicit partial, unsupported, stale, permission-hidden, quota, or redaction outcomes. | parent scope, asset family, content family, reason, outcome, retryability, read time. |

Facts must use `source_confidence=reported` for provider API data. Fixture facts
used in tests can use the same provider confidence because they model provider
responses, not local repository observations.

## Typed Depth Extractors

The parser drops the raw `resource.data` blob at the redaction choke point, but
each supported asset type can recover a bounded, redaction-safe slice of it
through a per-asset-type extractor registry
(`go/internal/collector/gcpcloud/extractor.go`). Each asset type registers an
`AssetAttributeExtractor` in its own `extractor_<type>.go` file via `init()`, so
new types fan out without a shared parser switch. The parser hands the extractor
the raw `resource.data` transiently; the extractor returns only:

- a bounded `attributes` map of control-plane fields usable for Terraform
  import/drift, edges, correlation, or monitoring (never secrets, data-plane
  content, raw policy JSON, IPs, or response bodies),
- `correlation_anchors` (parent resource names, KMS key names, and similar
  resource identifiers) for cross-source correlation, and
- typed `gcp_cloud_relationship` observations whose endpoints are CAI full
  resource names.

These land on the `gcp_cloud_resource` fact as the generic top-level `attributes`
map and `correlation_anchors` list. They are generic: a new asset type's
extractor populates them without a `gcp_cloud_resource` schema bump (the fields
themselves were added in schema 1.1.0). Data-plane locators (object paths inside
source URIs, request bodies) MUST be dropped — keep only resource identities
(bucket, dataset, KMS key names).

**BigQuery Table** (`bigquery.googleapis.com/Table`) is the reference extractor:
it captures table type, schema field count, time partitioning, clustering
fields, KMS key name, row/byte counts, and creation/expiration time; emits typed
parent Dataset, KMS-key, and external GCS-source edges; and surfaces dataset and
KMS resource names as correlation anchors.

**BigQuery Dataset** (`bigquery.googleapis.com/Dataset`) captures location,
default table and partition expiration, creation and last-modified time, the
default-encryption KMS key name, and a redaction-safe summary of the access ACL
(entry count, the bounded distinct role set, and distinct principal classes —
never a member identity; `allAuthenticatedUsers` surfaces as the `authenticated`
class so a broad grant is not hidden). It emits the typed default-encryption
KMS-key edge and, for authorized-resource ACL entries (`view`/`dataset`/
`routine`), typed `authorizes_view`/`authorizes_dataset`/`authorizes_routine`
edges to the shared Table/Dataset/Routine — these are resource references, not
member identities, so they are resolvable CAI endpoints and become
dataset-sharing edges and correlation anchors. The KMS key and each
authorized-resource name are correlation anchors. The dataset's own tables are
not enumerable from its `resource.data` (the child Table extractor emits the
parent edge), and IAM member identities (user/group/domain/serviceAccount) are
not resolvable CAI endpoints, so those ACL entries stay class-summary only.
Per-member fingerprints are out of scope for the typed-depth seam, which carries
no redaction key.

**Subnetwork** (`compute.googleapis.com/Subnetwork`) captures region, purpose,
role, private Google access, stack type, flow-logs enablement, creation time,
and the secondary range names and count; emits the typed parent VPC
`subnetwork_in_network` edge; and surfaces the parent network resource name as a
correlation anchor. Address-bearing fields are reduced to a redaction-safe
signal: the primary range is kept only as a prefix length (subnet size, not
address), and the gateway address, secondary range CIDRs, and IPv6 ranges are
dropped, so no public or private IP address reaches a fact.

**VPC Network** (`compute.googleapis.com/Network`) captures the auto-subnetwork
mode flag, routing mode, MTU, creation timestamp, and subnetwork/peering counts;
emits typed contained-subnetwork (`compute.googleapis.com/Subnetwork`) and VPC
peering (`compute.googleapis.com/Network`) edges; and surfaces the contained
subnetwork and peer network full resource names as correlation anchors. Only the
edges derivable from the Network resource itself are emitted here — firewalls,
routes, and attached instances reference the network from their own
`resource.data` and are emitted by those asset types' extractors. Legacy IPv4
ranges, gateway IPs, and peering public-IP export flags are data-plane fields and
are never decoded or surfaced.

**Forwarding Rule** (`compute.googleapis.com/ForwardingRule`) captures region
(omitted for a global rule), load-balancing scheme with a derived external
posture flag, IP protocol, port range or the port list, IP version, all-ports
and network-tier posture, and creation time; emits typed edges to the resolved
target (backend service, target pool, or one of the target-proxy kinds),
enclosing VPC network, and subnetwork; and surfaces each resolved target as a
correlation anchor. The `IPAddress` field is never decoded — mirroring the
Static Address extractor's treatment of its own reserved-address value — so no
public or private IP address reaches a fact; the external-vs-internal exposure
posture comes from `loadBalancingScheme`, never from the address.

**Backend Service** (`compute.googleapis.com/BackendService` and
`compute.googleapis.com/RegionBackendService`) captures protocol,
load-balancing scheme, port name, timeout, an explicit CDN-enabled tri-state
(distinguishing an explicit `false` from an absent field), session affinity,
region (omitted for a global backend service), a backend-entry count, and
creation time; emits a `backend_service_uses_security_policy` edge to the
Cloud Armor SecurityPolicy, a `backend_service_uses_edge_security_policy` edge
to the separate Cloud Armor edge SecurityPolicy (Compute exposes
`securityPolicy` and `edgeSecurityPolicy` as two distinct resource-URL
fields, so a backend service protected only by an edge policy still gets an
edge), a `backend_service_uses_health_check` edge to each HealthCheck, and a
shared `backend_service_has_backend` edge to each backend entry's
InstanceGroup or NetworkEndpointGroup (one relationship type for both group
kinds, distinguished by `target_asset_type`, mirroring the ForwardingRule
extractor's shared target-proxy relationship type); and surfaces the security
policy, edge security policy, each health check, and each resolved backend
group as correlation anchors. This is the other side of the edge the
ForwardingRule extractor already resolves toward
`compute.googleapis.com/BackendService` (a ForwardingRule's `backendService`
reference). The CAI search/analysis APIs report both regional and global
backend-service scope under the single `BackendService` asset type, but the
list/export/monitor/query path this collector uses emits regional backend
services under the distinct `RegionBackendService` asset type instead; both
asset types register to the same extractor function, mirroring
ForwardingRule/GlobalForwardingRule and Address/GlobalAddress. IAP
(Identity-Aware Proxy) OAuth client id/secret and CDN cache-key/signed-URL key
material are never decoded, and per-backend balancing-mode, capacity-scaler,
and max-utilization tuning fields are dropped by omission — only the `group`
reference is read from each backend entry.

**Health Check** (`compute.googleapis.com/HealthCheck`) captures protocol
type (HTTP/HTTPS/TCP/SSL/HTTP2/GRPC), check interval, timeout, healthy and
unhealthy thresholds, creation time, and the port plus port specification
read from whichever protocol-specific sub-object (`httpHealthCheck`,
`httpsHealthCheck`, `tcpHealthCheck`, `sslHealthCheck`, `http2HealthCheck`,
`grpcHealthCheck`) matches `type`; reuses `assetTypeComputeHealthCheck` from
the Backend Service extractor (`extractor_backend_service.go`), never
redeclaring it, since that extractor's `backend_service_uses_health_check`
edge already resolves toward this asset type as its target. A Health Check
emits no outbound edges or correlation anchors of its own — the graph value
runs the other direction, with each referencing BackendService owning the
edge toward its HealthCheck. `requestPath`, `host`, `response`,
`proxyHeader`, and `grpcServiceName` are data-plane routing/matching values
on the protocol sub-objects and are never decoded.

**URL Map** (`compute.googleapis.com/UrlMap`) captures a bounded host-rule
count, a bounded path-matcher count, the total path-rule count summed across
every path matcher, the total route-rule count summed across every path
matcher, and creation time; emits a `url_map_default_service` edge from the
map's own `defaultService`, a `url_map_path_matcher_default_service` edge
from each pathMatcher's `defaultService`, a `url_map_path_rule_service` edge
from each pathMatcher's `pathRules[].service`, a `url_map_route_rule_service`
edge from each pathMatcher's `routeRules[].service` (the advanced-routing
alternative/complement to `pathRules`), and a
`url_map_route_rule_weighted_service` edge from each entry of
`routeRules[].routeAction.weightedBackendServices[].backendService` (one route
rule can weight traffic across multiple backends, e.g. canary rollouts) —
each edge resolves to either `compute.googleapis.com/BackendService` or
`compute.googleapis.com/BackendBucket` depending on the referenced resource
segment; and surfaces every resolved backend reference as a correlation
anchor. `hostRules[].hosts`, `pathMatchers[].pathRules[].paths`, and
`pathMatchers[].routeRules[].matchRules` (plus routeAction's non-backend
traffic-shaping controls, such as `weight`) are data-plane routing patterns
and are never decoded — only the bounded counts and the resolvable
backend-service/backend-bucket references leave the parser.

**SSL Certificate** (`compute.googleapis.com/SslCertificate`) captures the
certificate type (`MANAGED`/`SELF_MANAGED`), the managed-certificate
provisioning status, a bounded managed-domain count, a bounded
subject-alternative-name count (present only for a self-managed certificate,
and only after issuance), expiry time, and creation time; emits no outbound
edges. An omitted `type` is derived to `SELF_MANAGED` — the value the Compute
sslCertificates schema defines for an absent type — rather than dropped, so a
self-managed certificate created without an explicit type still carries usable
Terraform/monitoring truth; deriving the type reads no key material. The certificate's graph value is inbound: a Target HTTPS Proxy or
Target SSL Proxy references it through its own `sslCertificates[]` field and
resolves the edge from that side, the same inbound-only edge shape as the
Custom IAM Role extractor. `managed.domains[]` and `subjectAlternativeNames`
are DNS-name-shaped values; the typed-depth extractor seam carries no
redaction key, so — mirroring the Managed Zone extractor's treatment of its
own `dnsName` and the reCAPTCHA Enterprise Key extractor's treatment of
allowed-domain entries — every domain value is reduced to a bounded count,
never persisted raw. The raw PEM certificate body and private key under
`selfManaged` are never decoded at all.

**Target HTTPS Proxy** (`compute.googleapis.com/TargetHttpsProxy`) captures
the QUIC negotiation override posture and creation time; emits a
`target_https_proxy_uses_url_map` edge to the resolved UrlMap and a
`target_https_proxy_uses_ssl_policy` edge to the resolved SslPolicy (omitted
when absent, since GCP applies its default TLS profile in that case). Its
serving certificate resolves through one of two mutually exclusive paths: when
`certificateMap` is set, a `target_https_proxy_uses_certificate_map` edge to
the Certificate Manager CertificateMap
(`certificatemanager.googleapis.com/CertificateMap`) is emitted and the classic
`sslCertificates` list is suppressed, because the Compute API ignores
`sslCertificates` when a certificate map is set (emitting a classic-certificate
edge there would surface a relationship GCP is not serving); otherwise each
`sslCertificates` entry is routed by domain — a Compute self-link becomes a
`target_https_proxy_uses_ssl_certificate` edge to the compute SslCertificate,
while a Certificate Manager self-link
(`//certificatemanager.googleapis.com/.../certificates/...`) becomes a
`target_https_proxy_uses_certificate_manager_certificate` edge to the
`certificatemanager.googleapis.com/Certificate`. The URL map, the SSL policy,
and whichever serving certificate(s) resolved are surfaced as correlation
anchors. The reverse edge from a ForwardingRule to this proxy is already
emitted by the ForwardingRule extractor (`forwarding_rule_targets_target_proxy`,
resolving toward `compute.googleapis.com/TargetHttpsProxy`): CAI's
TargetHttpsProxy resource.data carries no back-reference to the forwarding rule
that targets it, since the reference is one-directional, so this extractor emits
no forwarding-rule edge of its own. No certificate key material, private key,
or response body is ever decoded — `sslCertificates`, `certificateMap`, and
`sslPolicy` carry only resource self-links, never certificate content.

**Certificate Manager Certificate** (`certificatemanager.googleapis.com/Certificate`)
captures a certificate classification (`MANAGED`/`SELF_MANAGED`/`MANAGED_IDENTITY`,
derived from which of the mutually exclusive `managed`/`selfManaged`/
`managedIdentity` blocks is present, defaulting to `SELF_MANAGED` when none is
set — the same default the API applies to an uploaded certificate with no
management block), scope, the managed-certificate provisioning state, a
bounded managed-domain count, a bounded DNS-authorization count, a bounded
subject-alternative-name count, a bounded label count, and create/update/expiry
time; emits a `certificate_manager_certificate_uses_dns_authorization` edge for
each resolved `managed.dnsAuthorizations[]` entry toward the
`certificatemanager.googleapis.com/DnsAuthorization` asset type and a
`certificate_manager_certificate_uses_issuance_config` edge for a resolved
`managed.issuanceConfig` toward `certificatemanager.googleapis.com/CertificateIssuanceConfig`.
Neither target resource has its own typed-depth extractor yet, so this
extractor declares both asset type constants for a future extractor to reuse,
mirroring how the ForwardingRule extractor declares proxy-kind asset types for
reuse by their own eventual typed-depth extractors. It also resolves each
`usedBy[].name` entry — the resource actually consuming this certificate —
into a `certificate_manager_certificate_used_by_certificate_map_entry` edge
toward `certificatemanager.googleapis.com/CertificateMapEntry` (the
CertificateMap-served path; no other extractor emits an edge for this path,
since Certificate Manager has no typed-depth extractor for CertificateMapEntry
yet, so this extractor also declares that asset type constant) or a
`certificate_manager_certificate_used_by_target_https_proxy` edge toward the
directly-referencing `compute.googleapis.com/TargetHttpsProxy` (a duplicate of
the forward edge that extractor already emits via its own
`sslCertificates[]`/`certificateMap` resolution, since the reducer
materializes edges idempotently on repeated observation of the same logical
relationship); an unresolvable, blank, or wrong-domain `usedBy[].name` mints no
edge or anchor. `managed.domains[]`, `sanDnsnames`, and the managed-identity
`identity` SPIFFE ID are DNS-name- or workload-identity-shaped values; the
typed-depth extractor seam carries no redaction key, so — mirroring the SSL
Certificate extractor's treatment of its own `managed.domains[]` and
`subjectAlternativeNames` — every domain value and the SPIFFE ID are reduced
to bounded counts or presence only, never persisted raw. The top-level
`pemCertificate` and `selfManaged.pemCertificate`/`selfManaged.pemPrivateKey`
PEM certificate/key material, and
`managed.provisioningIssue`/`managed.authorizationAttemptInfo` free-text
failure detail, are never decoded.

**Filestore Instance** (`file.googleapis.com/Instance`) captures state, tier
(`BASIC_HDD`/`BASIC_SSD`/`ENTERPRISE`/`ZONAL`/`REGIONAL`/etc.), creation time,
a bounded file-share count, the first `networks[]` entry's connect mode
(`DIRECT_PEERING`/`PRIVATE_SERVICE_ACCESS`), the CMEK key name, and a bounded
label count; emits a typed `filestore_instance_in_network` edge for every
entry in `networks[]` toward the attached Compute `Network` (a bare short
network name is promoted to the project-less global partial and resolved the
same way the GKE Cluster extractor resolves its own network reference) and a
`filestore_instance_encrypted_by_kms_key` edge to the CMEK `CryptoKey` (an
already CAI-prefixed `kmsKeyName` value is kept as-is; a bare relative name is
prefixed, mirroring the Memorystore Redis Instance CMEK normalization). Each
resolved network and the CMEK key are surfaced as correlation anchors.
`networks[].reservedIpRange` and `networks[].modes` are never decoded — the
former is a CIDR range and the latter carries no typed-depth value — and no
per-file-share name or capacity is persisted, since `fileShares` is an
unbounded, caller-controlled array; only a bounded count crosses the parser
boundary. The typed-depth `attributes` map surfaces only a bounded
`label_count`, not the label entries themselves; the instance's labels are
still captured and, where sensitive, value-fingerprinted per
`redaction_policy_version` through the collector's shared label path, the same
as every sibling extractor — the extractor simply does not re-copy them into
typed depth.
**Spanner Instance** (`spanner.googleapis.com/Instance`) captures the instance
config short name (the trailing `instanceConfigs/<id>` segment, the
regional/multi-region topology such as `regional-us-central1` or
`nam-eur-asia1`), display name, node count or processing units (a Spanner
instance is provisioned by exactly one of the two capacity modes, so both are
decoded independently and neither implies the other's presence — an explicit
`0` is preserved rather than dropped, since the Spanner
`projects.instances` REST resource reports `0` for a `FREE_INSTANCE` and for a
standard instance still in the `CREATING` state, and dropping it would erase
real capacity evidence), lifecycle state, and a bounded label count; emits a
`spanner_instance_uses_instance_config` edge to the resolved InstanceConfig.
The instance config is itself a separately CAI-inventoried resource
(`spanner.googleapis.com/InstanceConfig`), so the `config` reference resolves to
a real typed edge target rather than staying an opaque attribute — the short
name is kept as the `config` attribute for Terraform/drift/monitoring, and the
full `//spanner.googleapis.com/projects/<p>/instanceConfigs/<id>` resource name
is emitted as the edge endpoint and correlation anchor (a bare config id on a
sparse page is qualified against the instance's own project; an already
CAI-prefixed reference is not double-prefixed). No CMEK edge is emitted: CMEK is
a per-database property (`encryptionConfig.kmsKeyName`) carried by the child
`spanner.googleapis.com/Database` asset type — a separate, not-yet-registered
typed-depth extractor — not by the Instance resource, so fabricating a KMS edge
here would assert a relationship the Instance does not carry (the child Database
extractor, when added, will own the CMEK edge, the same way the child Table
extractor owns the BigQuery Table→Dataset edge rather than the Dataset
enumerating its Tables). Raw label keys and values are never decoded by this
extractor; only the bounded label count crosses the redaction boundary, since
per-key/value fingerprinting is the base observation path's job (`parse.go`),
not the typed-depth extractor's. The Spanner Admin API's data-plane connection
endpoints (`endpointUris`) are intentionally not declared as a struct field at
all, so they are never decoded into Go memory in the first place.

**IAM Service Account** (`iam.googleapis.com/ServiceAccount`) captures unique id,
fingerprinted email, display name, OAuth2 client id, disabled posture, and a
bounded key count when the blob carries one (key material is never read). It
surfaces the fingerprinted email as its single correlation anchor and derives no
outbound edges from its own data: a service account's graph edges — impersonation
trust, IAM member bindings, "resources running as it", and key sub-resources —
are inbound and owned by the secrets/IAM trust facts and the
`container_image_identity` layer, which join onto this resource node through the
same `GCPServiceAccountEmailDigest` the anchor carries. The raw email is never
persisted as a captured attribute; it survives only verbatim inside the full
resource name kept for exact reducer joins.

**Persistent Disk** (`compute.googleapis.com/Disk`) captures zone or region,
size in GB, disk type, status, attached-instance count, physical block size, and
creation time; emits typed `disk_attached_to_instance` edges (one per attached
instance), `disk_created_from_image` / `disk_created_from_snapshot` provenance
edges, and a `disk_encrypted_by_key` edge to the encryption CryptoKey; and
surfaces those instance, image, snapshot, and KMS resource names as correlation
anchors. The KMS reference is reduced to its CryptoKey resource name (any
`cryptoKeyVersions` suffix is stripped), and the encryption key's `sha256`/raw
material fields are never decoded, so no key material reaches a fact.

**Route** (`compute.googleapis.com/Route`) captures the destination prefix length
and a default-route flag, priority, the next-hop gateway leaf name, a
next-hop-IP presence flag, network tags, and creation time; emits the typed
`route_in_network`, `route_next_hop_instance`, `route_next_hop_vpn_tunnel`, and
`route_next_hop_ilb` edges; and surfaces the enclosing network and each
resolvable next-hop resource (instance, VPN tunnel, or internal load balancer)
as correlation anchors. The destination range is reduced to a prefix length and
a default-route boolean, and the next-hop IP to a presence flag — no destination
CIDR or next-hop IP address reaches a fact. The next-hop internet gateway is not
a resolvable CAI asset, so its leaf name is kept as an attribute, not an edge.

**Compute Engine Instance** (`compute.googleapis.com/Instance`) captures machine
type, status, zone, scheduling posture (preemptible, automatic-restart,
on-host-maintenance, provisioning model), Shielded VM posture (secure boot, vTPM,
integrity monitoring), deletion protection, IP-forwarding, creation time,
service-account count and scopes, network-interface and disk counts, the boot-disk
presence flag, network tags, and instance metadata **keys**; emits typed
`instance_uses_disk` edges (one per attached disk), and the `instance_in_network`
and `instance_in_subnetwork` edges; and surfaces the attached disk, interface
network and subnetwork resource names plus the fingerprinted service-account email
as correlation anchors. External-IP exposure is reduced to a `has_external_ip`
signal and an external-access-config count — no `natIP` or `networkIP` value is
ever decoded into a fact. Metadata **values** (startup scripts, SSH keys, env
values) and the raw service-account email are never persisted; the service
account is an anchor (its "runs as" edge is inbound, owned by the secrets/IAM and
image-identity layers keying on the same `GCPServiceAccountEmailDigest`), not an
edge, because an email is not an exactly resolvable CAI endpoint.

**Firewall Rule** (`compute.googleapis.com/Firewall`) captures direction,
priority, disabled and log-config posture, the allow/deny protocols and ports,
source/destination range counts, an `opens_to_public` exposure signal, target
network tags, and the target service-account count; emits the typed
`firewall_applies_to_network` edge; and surfaces the enclosing network resource
name plus the fingerprinted target service-account emails as correlation anchors.
Source and destination ranges are reduced to counts and the public-exposure
boolean (a `0.0.0.0/0` or `::/0` entry): the CIDR values themselves are never
persisted, so no public or private IP address reaches a fact. Target service
accounts are anchors, not edges — an email is not an exactly resolvable CAI
endpoint, and the "applies to instances running as SA" join is owned by the
secrets/IAM layer keying on the same `GCPServiceAccountEmailDigest`.

**Static Address** (`compute.googleapis.com/Address` and the global
`compute.googleapis.com/GlobalAddress`, both handled by the same extractor)
captures region, address
type, an `is_external` exposure flag, purpose, status, IP version, creation time,
and the count of distinct resolvable using resources (forwarding rules and
instances); emits the typed `address_in_network`,
`address_in_subnetwork`, `address_used_by_forwarding_rule`, and
`address_used_by_instance` edges; and surfaces the enclosing network and
subnetwork plus each resolvable using resource (forwarding rule or instance) as
correlation anchors. The reserved IP `address` value is never decoded at all —
the external-vs-internal posture comes from `addressType`, so no public or
private IP address reaches a fact.

**Artifact Registry DockerImage**
(`artifactregistry.googleapis.com/DockerImage`) captures the pullable image URI,
the content digest, tags and tag count, image size, media type, and build/upload
time; emits the typed parent `docker_image_in_repository` edge; and surfaces the
parent repository resource name and the content digest as correlation anchors
(the digest is a content-addressable join key that image-identity correlation
can key on). Only
control-plane identifiers and metadata are kept — no layer content or raw
manifest body. Built-by Build and deployed-to Run/GKE edges are intentionally
not emitted from this asset: they are cross-source correlations keyed on the
digest from the deploying resource's own image references, so this extractor
never fabricates an endpoint it cannot resolve from `resource.data`.

**Secret Manager Secret** (`secretmanager.googleapis.com/Secret`) captures
replication type (automatic or user-managed) and user-managed location count,
customer-managed-encryption posture, rotation period and next rotation time,
expiration (expire time or ttl), creation time, and the topic and version-alias
counts; emits the typed `secret_encrypted_by_kms_key` edge to each CMEK
`CryptoKey` and the `secret_notifies_topic` edge to each rotation-notification
Pub/Sub `Topic`; and surfaces the CMEK key and topic resource names as
correlation anchors. No secret payload is ever read — payloads live on separate
`SecretVersion` resources outside the CAI Secret asset — and only key/topic
resource names (control-plane identifiers, not key material or values) leave the
parser.

**Pub/Sub Topic** (`pubsub.googleapis.com/Topic`) captures lifecycle `state`,
customer-managed-encryption posture, message-storage region residency
(`allowedPersistenceRegions`, deduplicated and sorted) and in-transit
enforcement, message-schema encoding, and message retention duration; emits the
typed `topic_encrypted_by_kms_key` edge to the CMEK `CryptoKey` and the
`topic_uses_schema` edge to the message `Schema`; and surfaces the CMEK key and
schema resource names as correlation anchors. Subscription and IAM
publisher/subscriber edges are intentionally not emitted from this asset: a
topic's own `resource.data` carries neither its subscriptions (each references
the topic on its own asset) nor its IAM policy (never persisted), so those joins
belong to the subscription-asset and IAM-policy paths, and this extractor never
fabricates an endpoint it cannot resolve from `resource.data`.
**Custom IAM Role** (`iam.googleapis.com/Role`) captures the role title, launch
stage, included-permission count, the count of privilege-escalation-relevant
permissions with a `grants_privilege_escalation` flag, the deleted posture, and a
fingerprinted etag. The individual permission strings are reduced to bounded
counts and flags rather than surfaced verbatim, and the opaque etag is reduced to
a stable digest so no raw concurrency token leaves the parser. The role's edges —
the members bound to it and its owning project/org — are inbound and owned by the
IAM/binding and ancestry layers, which join on the role identity and the ancestry
already carried on the base observation; the extractor derives no outbound edges
or anchors from the role's own data.

**Cloud Run Service** (`run.googleapis.com/Service`) captures ingress setting,
execution environment, VPC egress posture, per-revision scaling min/max instance
counts and the service-level scaling floor/mode (both `template.scaling` and the
top-level `scaling` block, as distinct keys), latest ready revision, creation
time, the container count, the bounded set of
container env variable **keys** (never values) with their count, and the mounted
secret count; emits the typed `run_service_uses_vpc_connector` edge to the
Serverless VPC Access `Connector` and a `run_service_mounts_secret` edge to each
mounted Secret Manager `Secret` (from both secret volumes and env
`secretKeyRef` sources); and surfaces the connector and secret resource names as
correlation anchors. The runtime service account is carried only as a
fingerprinted-email digest (attribute and anchor) so the IAM/trust layer joins
the inbound "runs as" edge without the raw email ever being persisted; container
images continue to flow through the shared image-reference path. No env value is
ever read, and only control-plane resource names and references leave the parser.

**Pub/Sub Subscription** (`pubsub.googleapis.com/Subscription`) captures
lifecycle `state`, delivery type (push / pull / bigquery / bigtable /
cloud_storage), push
endpoint scheme (an http-vs-https alerting signal) and a deterministic
fingerprint of the push endpoint host, ack deadline, acked-message retention and
message retention duration, expiration ttl, exactly-once delivery, dead-letter
max delivery attempts, and whether a message filter is set; emits the typed
`subscription_subscribes_to_topic` and `subscription_dead_letters_to_topic`
edges to their `Topic`s, `subscription_exports_to_bigquery_table` to the export
`Table`, `subscription_exports_to_bigtable_table` to the export Bigtable
`Table`, and `subscription_exports_to_storage_bucket` to the export `Bucket`;
and surfaces the subscribed topic, dead-letter topic, export table (BigQuery or
Bigtable), and export bucket resource names as correlation anchors. The push endpoint is never persisted raw — its
path and query (which can carry OIDC tokens or shared secrets) are dropped, the
host is reduced to a fingerprint (matching the DNS-name redaction posture), and
the filter expression is recorded only as a presence flag because it can
reference message attribute names and values.

**Cloud Run Revision** (`run.googleapis.com/Revision`) is the immutable deployed
version of a Service, so its container, service-account, scaling, VPC, and secret
fields sit at the top level of `resource.data`. It captures execution
environment, VPC egress posture, per-revision scaling min/max instance counts,
the primary container image reference and its sha256 digest, creation time, the
Ready condition state, the container count, the bounded set of container env
variable **keys** (never values) with their count, and the mounted secret count;
emits the typed `revision_of_service` edge to the parent Service (derived from
the revision's own full resource name), the `revision_uses_vpc_connector` edge to
the Serverless VPC Access `Connector`, and a `revision_mounts_secret` edge to
each mounted Secret Manager `Secret`; and surfaces the parent Service, connector,
secret, image digest, and fingerprinted runtime service-account email as
correlation anchors. Unlike the Service extractor, the Revision owns its
container images (the shared image-reference path covers only Service and Job
assets). The runtime service account is carried only as a fingerprinted-email
digest so the IAM/trust layer joins the inbound "runs as" edge without the raw
email ever being persisted. No env value is ever read, and only control-plane
resource names and references leave the parser.

**IAM Service Account Key** (`iam.googleapis.com/ServiceAccountKey`) captures the
key type (USER_MANAGED/SYSTEM_MANAGED), key algorithm, key origin, the
valid-after/valid-before window (age and rotation posture), and the disabled
posture; derives the parent ServiceAccount from the key's own full resource name
and emits the typed `service_account_key_of` edge to it, with the parent
service-account email (fingerprinted) as the single correlation anchor. Private
and public key material (`privateKeyData`, `publicKeyData`, `privateKeyType`) is
never read — only control-plane posture and the fingerprinted parent identity
leave the parser.

**Firestore Database** (`firestore.googleapis.com/Database`) captures the
database mode (`FIRESTORE_NATIVE` / `DATASTORE_MODE`), location id, concurrency
mode, App Engine integration mode, point-in-time-recovery and delete-protection
posture, customer-managed-encryption posture, and creation time; emits the typed
`firestore_database_encrypted_by_kms_key` edge to the CMEK `CryptoKey`; and
surfaces the CMEK key resource name as a correlation anchor. Only control-plane
metadata and the CMEK key resource name (not key material) leave the parser; no
document data is read.

**IAM Workload Identity Pool** (`iam.googleapis.com/WorkloadIdentityPool`) captures
the lifecycle state and disabled posture. The pool's external-trust value — its
providers and the AWS/OIDC trust they grant — lives on its child provider
resources, which are inbound to the pool, so the pool derives no outbound edges
of its own.

**IAM Workload Identity Pool Provider**
(`iam.googleapis.com/WorkloadIdentityPoolProvider`) captures the external trust
type (aws / oidc / saml / x509) and its bounded trust anchor — the AWS account id
or the OIDC issuer URI, both cross-cloud/OIDC correlation join keys — plus the
attribute-mapping key count (the effective count, including IAM's default two-key
mapping for a bare AWS provider), an attribute-condition presence flag, and the
lifecycle/disabled posture; it emits the typed `workload_identity_provider_of_pool`
edge to the parent pool (derived from the provider's own full resource name) with
the trust anchor as the correlation anchor. OIDC inline JWKS key material
(`oidc.jwksJson`), SAML IdP metadata (`saml.idpMetadataXml`), and the X.509
trust-store certificates (`x509.trustStore`) are never read,
and attribute-mapping values and the attribute-condition CEL expression (which can
reference asserted claim names and values) are never persisted — only the mapping
key count and a presence flag.

**Dataproc Cluster** (`dataproc.googleapis.com/Cluster`) captures lifecycle
`status.state`, internal-IP-only posture, master and worker machine type and
instance counts, image version, customer-managed-encryption and autoscaling
posture, and the fingerprinted runtime service-account email; emits the typed
`dataproc_cluster_uses_network` and `dataproc_cluster_uses_subnetwork` edges to
the cluster's `Network` and `Subnetwork`, `dataproc_cluster_encrypted_by_kms_key`
to the persistent-disk CMEK `CryptoKey`, and `dataproc_cluster_uses_staging_bucket`
to the staging `Bucket`; and surfaces the network, subnetwork, CMEK key, staging
bucket, and fingerprinted service-account email as correlation anchors. The
runtime service account is carried only as a fingerprinted-email digest (an email
is not an exactly resolvable CAI endpoint), software properties and
initialization actions are not decoded, and only control-plane resource names and
references leave the parser.

**GKE Cluster** (`container.googleapis.com/Cluster`) captures location, status,
master/node version, release channel, create time, private-cluster and
master-authorized-networks posture (a bounded CIDR-block count, never the CIDR
values), workload identity pool, addon posture, and a per-node-pool summary
(machine type, fingerprinted node service-account email, OAuth-scope count,
autoscaling posture, initial node count); emits the typed
`gke_cluster_uses_network` and `gke_cluster_uses_subnetwork` edges to the
cluster's `Network` and `Subnetwork`; and surfaces the network, subnetwork, and
per-node-pool fingerprinted service-account emails as correlation anchors.
Master-authorized-network CIDR values and node-pool OAuth scope values are
never decoded, and the GKE API's "default" service-account sentinel is never
fingerprinted or anchored since it does not identify a specific account.

**Secret Manager Secret Version**
(`secretmanager.googleapis.com/SecretVersion`) captures the lifecycle state
(ENABLED / DISABLED / DESTROYED), create and destroy times, replication type
(automatic / user-managed), and customer-managed-encryption posture; derives the
parent Secret from the version's own full resource name and emits the typed
`secret_version_of_secret` edge to it plus a
`secret_version_encrypted_by_kms_key_version` edge to each CMEK `CryptoKeyVersion`
(with those key version resource names as correlation anchors). The secret payload
is never read — only control-plane posture and KMS key version resource names
(not key material) leave the parser.
**Cloud Functions Function** (`cloudfunctions.googleapis.com/Function`) unions
the gen1 and gen2 shapes (the `environment` field distinguishes them; gen2 nested
`buildConfig`/`serviceConfig` fields are preferred over the gen1 top-level
fields). It captures environment, lifecycle state, build runtime, ingress
settings, VPC egress posture, event-trigger event type, the mounted secret count,
and update time; emits the typed `function_source_bucket` edge to the source
archive's Storage `Bucket`, `function_uses_vpc_connector` to the Serverless VPC
Access `Connector`, `function_mounts_secret` to each mounted Secret Manager
`Secret`, and `function_triggered_by_topic` to the event-trigger Pub/Sub `Topic`;
and surfaces the source bucket, connector, secrets, trigger topic, and the
fingerprinted runtime and trigger service-account emails as correlation anchors.
Service accounts are carried only as fingerprinted-email digests so the IAM/trust
layer owns the inbound "runs as" edges. The source object path (from the gen2
`storageSource` object or the gen1 `gs://` `sourceArchiveUrl`) is dropped —
only the bucket is kept — and the https trigger URL, secret values, and env
values are never read.

**BigQuery Routine** (`bigquery.googleapis.com/Routine`) captures the routine
type (`SCALAR_FUNCTION` / `PROCEDURE` / `TABLE_VALUED_FUNCTION`), language,
argument count, return type kind, definition-body presence, imported-library
count, and creation time; emits the typed `bigquery_routine_in_dataset` edge to
the parent `Dataset` and `bigquery_routine_uses_connection` to the
remote-function `Connection`; and surfaces the parent dataset and connection
resource names as correlation anchors. The routine's `definitionBody` (user
SQL/JavaScript source) is never read — only its presence is recorded — so table
references embedded in that body are intentionally not turned into edges; only
structured control-plane references leave the parser.

**API Key** (`apikeys.googleapis.com/Key`) captures the display name, creation
time, the configured key restriction type (browser / server / android / ios), and
the restricted API-target services (count plus the bounded, sorted service-name
list). The secret key string (`keyString`) is never read, and every restriction
value — allowed IPs, referrer URLs, Android app fingerprints, iOS bundle ids — is
reduced to a presence-only restriction-type signal so no address, URL, or app
identifier leaves the parser. For an authorization key it also surfaces the
fingerprinted service-account email the key authenticates as (never the raw
address), as the cross-source IAM/trust join anchor. The owning project is
base-observation ancestry and the restricted API targets are GCP service
identifiers rather than resolvable CAI resources, so the extractor emits no
outbound edges.

**Dataform Repository** (`dataform.googleapis.com/Repository`) captures the git
default branch, a fingerprint of the git remote host, the fingerprinted runtime
service-account email, the workspace-compilation default database, customer-
managed-encryption posture, and creation time; emits the optional typed
`dataform_repository_encrypted_by_kms_key` edge to the CMEK `CryptoKey`; and
surfaces the CMEK key, git-remote host fingerprint, and service-account email
fingerprint as correlation anchors. The git remote is an external (non-CAI)
endpoint reduced to a host fingerprint (URL path and any embedded credentials
dropped); the git auth-token / npmrc secret versions and SSH key material are
never decoded; and the service account, workspaces, and compiled BigQuery
datasets are carried as a fingerprint anchor or owned by their own child/derived
assets rather than becoming edges from the repository's own `resource.data`.
**Cloud Functions (gen1)** (`cloudfunctions.googleapis.com/CloudFunction`) is the
first-generation function resource (the v1 API type, distinct from the
gen2/unified `Function`). It captures lifecycle status, runtime, entry point,
available memory, ingress settings, VPC egress posture, trigger type
(https or event) and event type, the mounted secret count, update time, and the
fingerprinted runtime service-account email; emits the typed
`function_source_bucket` edge to the source archive's Storage `Bucket`,
`function_uses_vpc_connector` to the Serverless VPC Access `Connector` (a bare
gen1 connector name is qualified with the function's own project and location),
`function_mounts_secret` to each mounted Secret Manager `Secret`, and
`function_triggered_by_topic` to the event trigger's Pub/Sub `Topic`; and
surfaces the source bucket, connector, secrets, trigger topic, and fingerprinted
runtime service-account email as correlation anchors. The runtime service account
is carried only as a fingerprinted-email digest, the source object path from the
`gs://` `sourceArchiveUrl` is dropped (only the bucket is kept), and the https
trigger URL, secret values, and env values are never read.

**reCAPTCHA Enterprise Key** (`recaptchaenterprise.googleapis.com/Key`) captures
the display name, creation time, platform type (web / android / ios / express),
the web integration type, the per-platform allow-all posture and allow-list
counts, and the bounded WAF service/feature enums. The platform allow-list entries
themselves — web domains, Android package names, iOS bundle ids — can name
internal domains and applications, so they are only counted, never surfaced. The
owning project is base-observation ancestry and the platform settings are bounded
posture on the resource, so the extractor derives no outbound edges or anchors.

**BigQuery Data Transfer Config**
(`bigquerydatatransfer.googleapis.com/TransferConfig`) captures the data source
id, schedule, lifecycle state, disabled posture, customer-managed-encryption
posture, and the fingerprinted owner email; emits the typed
`transfer_config_writes_to_dataset` edge to the destination `Dataset`,
`transfer_config_notifies_topic` to the notification Pub/Sub `Topic`, and
`transfer_config_encrypted_by_kms_key` to the CMEK `CryptoKey`; and surfaces the
destination dataset, notification topic, CMEK key, and owner email fingerprint
as correlation anchors. The owner identity comes from `ownerInfo.email` (the
identity the transfer runs as — a service account when configured with one, a
user otherwise); `serviceAccountName` is a create/patch request parameter, not a
returned resource field, so it is not used. The `params` map (user query text, source
object paths, and other data-source-specific values) is never read, and the data
source is an enumerated source id rather than a resolvable CAI resource so it is
kept only as an attribute. The `transfer_config_writes_to_dataset` edge resolves
the destination dataset against the transfer config's own project:
`destinationDatasetId` is a bare dataset id with no project qualifier, and the
BigQuery Data Transfer resource surfaces no separate destination-project field
anywhere in its schema (verified against the live BigQuery Data Transfer v1
discovery document and the googleapis `transfer.proto`). A cross-region or
cross-project transfer config is created inside its destination project — its own
resource name embeds that project — so the config's own project is the
destination project by GCP's resource model, and this holds for both same-project
and cross-project copy configs. For a `cross_region_copy` config the only second
project that appears is the source project inside `params`, which is never
decoded and never resolves the destination edge. There is therefore no
cross-project destination signal in Cloud Asset Inventory for the collector to
prefer; this is a verified bound, not an unresolved gap.

**Cloud Build Build** (`cloudbuild.googleapis.com/Build`) captures build status,
create and finish time, the log URL host (host only), source type (repo or
storage), the output image count, and the fingerprinted build service account;
emits the typed `build_triggered_by` edge to the `BuildTrigger` (derived from the
build's `buildTriggerId` and its own project/location), `build_source_repo` to
the Cloud Source Repositories `Repository`, and `build_source_bucket` to the
source archive's Storage `Bucket`; and surfaces the trigger, source repo/bucket,
output image digests, and fingerprinted build service-account email as
correlation anchors. Output image digests feed container-image identity
correlation as anchors rather than direct edges. Build substitutions (which can
carry secrets), build logs, and the log/source object paths are never read — only
the log URL host and structured control-plane references leave the parser.

**Cloud Build Trigger** (`cloudbuild.googleapis.com/BuildTrigger`) captures the
user-assigned trigger `name`, disabled posture, creation time, build-config
`filename`, the API's own `eventType` enum, a bounded `source_type` (`repo`,
`github`, `repository_event`, `source_to_build`, `pubsub`, `webhook`, or
`manual`) derived from which mutually-exclusive source field the trigger
carries, the `includeBuildLogs` posture, the `approvalConfig.approvalRequired`
posture, bounded `includedFiles`/`ignoredFiles`/`tags` counts, and the
fingerprinted trigger service account (`tags` is free-form user text, unlike
the shared `labels` map, so only its count is kept, never the tag strings);
emits the typed `trigger_source_repo` edge to the Cloud Source Repositories
`Repository` for a `triggerTemplate`-sourced trigger
(reusing `assetTypeCloudBuildTrigger` and `assetTypeSourceRepo`, both declared
by the Cloud Build Build extractor, and the same repoName/projectId-with-
same-project-default resolution as a Build's `repoSource`); and surfaces the
source repo and fingerprinted service-account email as correlation anchors. A
GitHub, GitLab Enterprise, Bitbucket Server, Pub/Sub, webhook, Repo API, or
manual source has no CAI-resolvable target asset type in this graph — GitHub
owner/name, webhook state, and Pub/Sub topic/subscription are not emitted as
edges or attributes — so only the bounded `source_type` enum records which
kind of source the trigger uses. The trigger's `build` template,
`substitutions`, the free-text CEL `filter`, `webhookConfig.secret` (a Secret
Manager version reference used only to validate inbound webhook signatures),
GitHub/GitLab/Bitbucket push and pull-request branch/tag regex detail, and
`gitFileSource` are never read — only the bounded control-plane posture and
the resolvable source-repo reference leave the parser.

**Identity Platform Config** (`identitytoolkit.googleapis.com/Config`) captures
the authentication posture: the enabled sign-in methods (email / phone /
anonymous), the MFA state, the multi-tenant toggle, and the count of authorized
domains. The authorized-domain values (which can name internal hosts) are only
counted, and OAuth/IdP client secrets, API keys, and blocking-function URIs are
never read. The owning project is base-observation ancestry, and the IdP configs
and domains are child sub-resources or domain strings that are not resolvable CAI
resources, so the extractor derives no outbound edges or anchors.

**Artifact Registry Repository** (`artifactregistry.googleapis.com/Repository`)
captures the repository format (DOCKER/MAVEN/NPM/etc), mode, size in bytes,
cleanup-policy count, customer-managed-encryption posture, and creation time;
emits the typed `artifact_registry_repository_encrypted_by_kms_key` edge to the
CMEK `CryptoKey`; and surfaces the CMEK key resource name as a correlation anchor.
The images and packages the repository contains reference it from their own child
assets (see the DockerImage `docker_image_in_repository` edge) rather than being
enumerated here, so no contained-artifact edges are emitted from the repository's
own data. Only the CryptoKey resource name (a control-plane identifier, not key
material) and bounded control-plane metadata leave the parser.
**Dataplex Entry Group** (`dataplex.googleapis.com/EntryGroup`) captures the
catalog transfer status and creation time (the EntryGroup resource has no
lifecycle state field). An entry group is a container: its contained entries
reference it from their own assets (inbound) and its project is base-observation
placement, so the extractor derives no outbound edges or correlation anchors —
only bounded posture attributes. The extractor decodes no free-text description
and adds no display-name attribute (the base observation carries the provider
display name for every asset type).

**Eventarc Trigger** (`eventarc.googleapis.com/Trigger`) captures the matched
CloudEvents `type`, the event-filter count, the destination type
(run/function/workflow), the transport type, creation time, and the fingerprinted
trigger service account; emits the typed destination edge —
`trigger_targets_service` to the Cloud Run `Service` (the short destination
service name qualified with the trigger's project and the destination region),
`trigger_targets_function` to the Cloud Functions `Function`, or
`trigger_targets_workflow` to the `Workflow` — plus `trigger_transport_topic` to
the transport Pub/Sub `Topic` and `trigger_uses_channel` to the Eventarc
`Channel`; and surfaces the destination target, transport topic, channel, and
fingerprinted service-account email as correlation anchors. The service account
is carried only as a fingerprinted-email digest; the destination http-endpoint URI
and the Cloud Run destination path are never read.
**Logging Log Bucket** (`logging.googleapis.com/LogBucket`) captures the lifecycle
state (LogBucket reports it under `lifecycleState`, which the base observation does
not map), the retention period (days), the locked and analytics-enabled posture,
creation time, and CMEK posture; emits the typed `log_bucket_encrypted_by_kms_key` edge to the CMEK
`CryptoKey` with the key resource name as the correlation anchor. The bucket's
linked analytics BigQuery datasets are separate `Link` sub-resources (not on the
bucket's own `resource.data`) and its owning project is base-observation ancestry,
so the CMEK key is the only outbound edge; only the CryptoKey resource name (not
key material) leaves the parser.

**Logging Log Sink** (`logging.googleapis.com/LogSink`) captures the destination
type (storage / bigquery / pubsub / logging), filter presence, disabled posture,
exclusion count, creation time, and the fingerprinted writer-identity
service-account email; emits the typed export edge to the destination resource —
`log_sink_exports_to_bucket` (Storage `Bucket`), `log_sink_exports_to_dataset`
(BigQuery `Dataset`), `log_sink_exports_to_topic` (Pub/Sub `Topic`), or
`log_sink_exports_to_log_bucket` (Log `Bucket`) — and surfaces the destination
resource name and writer-identity digest as correlation anchors. The filter
expression (which can reference internal log, project, and resource names) is
reduced to a presence flag, and the writer-identity email is reduced to a digest;
neither raw value leaves the parser.

**Cloud Scheduler Job** (`cloudscheduler.googleapis.com/Job`) captures the cron
schedule, time zone, state, target type (pubsub/http/app_engine), HTTP method,
retry count, last attempt time, an HTTP target host fingerprint, and the
fingerprinted target service account; emits the typed
`scheduler_job_targets_topic` edge to the Pub/Sub `Topic` for a Pub/Sub target;
and surfaces the topic, HTTP host fingerprint, and fingerprinted service-account
email as correlation anchors. HTTP and App Engine targets resolve no CAI-asset
edge (an external host / an App Engine service). The Pub/Sub message payload and
attributes, the HTTP target URI (reduced to a host fingerprint), the OIDC
audience, and request headers are never read.

**Cloud Tasks Queue** (`cloudtasks.googleapis.com/Queue`) captures the queue
state, rate limits (max dispatches per second, max concurrent dispatches, max
burst size), retry max attempts, the App Engine routing-override service, purge
time, an HTTP target host fingerprint (host only, any port stripped), and the
fingerprinted HTTP-target service account; and surfaces the HTTP host
fingerprint and fingerprinted service-account email as correlation anchors. It
emits no typed edge: a CAI Queue full resource name carries the numeric project
number, which does not match the App Engine application id in an App Engine
`Service` full resource name, so a constructed routing-override edge would never
resolve — the routing service is kept as a bounded attribute instead. A
queue-level HTTP target likewise resolves no CAI-asset edge (an external host);
the HTTP URI-override path, OIDC audience, and header overrides are never read.

**App Engine Application** (`appengine.googleapis.com/Application`) captures
the location id, serving status, default GCS bucket name, default hostname
(host only — a public `appspot.com` hostname; it is already host-only with no
path or query, so it is stored verbatim), database type, and creation time;
emits the typed `application_uses_default_bucket` edge to the default Cloud
Storage `Bucket` (built with the canonical CAI Bucket prefix so the endpoint
resolves exactly); and surfaces the bucket full resource name as the
correlation anchor. The application name and id are base-observation identity
fields and are not decoded into attributes. The owning project is base-observation
ancestry; the default bucket is the only resolvable outbound CAI endpoint from
the application's own `resource.data`.
**App Engine Service** (`appengine.googleapis.com/Service`) captures the service
ID and traffic-split posture: the shard strategy (`split_shard_by`), the count
of version allocations (`version_count`), and the per-version allocation
percentages (`traffic_allocations`, a sorted `versionID=percentage` string slice
so the values survive the cloud-inventory readback sanitizer). Version IDs are
control-plane identifiers and are safe to store verbatim; blank keys are skipped.
For each allocation key the extractor emits one typed
`service_splits_traffic_to_version` edge to the corresponding
`appengine.googleapis.com/Version` resource, whose full resource name is derived
by appending `/versions/{versionID}` to the service full resource name; the
version full resource names are also surfaced as correlation anchors. The raw
`name` field, service configuration (env, scaling, handlers), and data-plane
content are not decoded.

**Cloud DNS Managed Zone** (`dns.googleapis.com/ManagedZone`) captures
visibility (public/private), DNSSEC state, creation time, the resolvable
private-visibility network count, and forwarding/peering posture (forwarding
enabled plus target count, and an `is_peering_zone` flag); emits the typed
`dns_managed_zone_visible_from_network` edge to each resolvable
`privateVisibilityConfig.networks[]` VPC `Network` and the
`dns_managed_zone_peers_with_network` edge to the `peeringConfig.targetNetwork`
peer `Network`; and surfaces those network resource names as correlation
anchors. The zone's own `dnsName` is never decoded into an attribute: it is DNS
name text exactly like the record name/target values the sibling
`gcp_dns_record` fact family fingerprints, and the typed-depth extractor seam
carries no redaction key, so it is omitted rather than persisted raw. Forwarding
target name servers (`forwardingConfig.targetNameServers[]`, which carry literal
IPv4/IPv6 addresses and hostnames) are read only to produce a bounded count and
an enabled flag — no address or hostname value ever leaves the parser. This
extractor is distinct from the `dns.googleapis.com/ResourceRecordSet` asset
type, whose record observations flow through the separate `gcp_dns_record` fact
family (`dns_record.go`), not this typed-depth seam.

**Cloud DNS Policy** (`dns.googleapis.com/Policy`) captures inbound-forwarding
posture (`enable_inbound_forwarding`) and logging posture (`enable_logging`) as
explicit tri-state booleans — a real `false` reported by the Cloud DNS v1 API
is kept distinct from the field being entirely absent from a partial CAI page,
mirroring the Backend Service extractor's `enable_cdn` treatment — plus the
resolvable bound-network count and the alternative-name-server count; emits
the typed `dns_policy_applies_to_network` edge to each resolvable
`networks[].networkUrl` VPC `Network`, surfacing those network resource names
as correlation anchors. The policy's own `description` is never decoded into
an attribute: it is free-form operator text, not a bounded control-plane field
usable for Terraform import/drift, edges, correlation, or monitoring, mirroring
the Managed Zone extractor's treatment of its own `dnsName`. Alternative name
server addresses (`alternativeNameServerConfig.targetNameServers[].ipv4Address`
/ `.ipv6Address`) are read only to produce a bounded count — no address value
ever leaves the parser. This extractor is distinct from the
`dns.googleapis.com/ManagedZone` asset type above: a Policy binds
inbound-forwarding, logging, and alternative-name-server behavior to a set of
VPC networks, while a ManagedZone is a DNS namespace with its own visibility
and peering configuration.

**Cloud Storage Bucket** (`storage.googleapis.com/Bucket`) captures placement
(location, location type), storage class, timestamps, uniform-bucket-level-access
and public-access-prevention posture, versioning, a bounded lifecycle-rule
count, and retention-policy posture (retention period and lock state); emits the
typed `storage_bucket_encrypted_by_kms_key` edge to the CMEK Cloud KMS
`CryptoKey` and the `storage_bucket_logs_to_bucket` usage-logging export edge to
the destination log bucket; and surfaces the KMS key resource name and the
logging destination bucket's full resource name as correlation anchors. The
bucket's `acl`, `defaultObjectAcl`, and `iamConfiguration.bucketPolicyOnly`
legacy IAM policy fields, object contents, and notification/pubsub
configuration are never decoded.

Performance Evidence: this extractor adds no new hot path. It is a pure
in-process function over one already-parsed, bounded CAI `resource.data` JSON
blob (no loop over external data, no Cypher, no graph or Postgres write, no
worker/lease/queue, no live provider call); cost is O(1) per bucket asset.
No-Observability-Change: extraction outcomes are covered by the existing
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` counters; no new metric is needed.

**KMS CryptoKey** (`cloudkms.googleapis.com/CryptoKey`) captures purpose
(`ENCRYPT_DECRYPT`/`ASYMMETRIC_SIGN`/`ASYMMETRIC_DECRYPT`/`MAC`), the version
template's protection level and algorithm, the rotation schedule
(`rotation_period`/`next_rotation_time`, present only for keys that rotate),
the primary version's lifecycle state, and creation time. Cloud KMS does not
report the containing KeyRing as a separate field on the CryptoKey's
`resource.data`, so the extractor derives it from the CryptoKey's own
resource-name path and emits the typed `kms_crypto_key_in_key_ring` edge to
the `cloudkms.googleapis.com/KeyRing` parent, surfacing the KeyRing full
resource name as the correlation anchor. Cloud KMS never returns key
material, key state history, or any data-plane content on this resource, and
none is read here.

**KMS KeyRing** (`cloudkms.googleapis.com/KeyRing`) captures location (derived
from the resource-name path — Cloud KMS reports no separate location field)
and creation time. Per the live Cloud KMS v1 `projects.locations.keyRings`
REST reference, the KeyRing resource carries only `name` and `createTime`, no
encryption, label, or child-key field of its own. The extractor emits no
outbound edges or correlation anchors: every contained CryptoKey already
resolves the `kms_crypto_key_in_key_ring` edge toward this asset type from the
CryptoKey side, so the KeyRing's graph value is inbound only. No key material,
IAM policy, or data-plane content is ever read here.

**Cloud SQL Instance** (`sqladmin.googleapis.com/Instance`) captures database
version, region, state, instance type, tier, availability type, data disk size,
public-IP posture (`ipv4Enabled`), SSL mode, backup and point-in-time-recovery
posture, transaction-log retention days, CMEK key name, replica count, and
creation time; emits the typed `sql_instance_in_network` edge to the private
Compute `Network`, the `sql_instance_encrypted_by_kms_key` edge to the CMEK
`CryptoKey`, and the replica-topology edges — `sql_instance_has_replica` from a
primary to each entry in `replicaNames` and `sql_instance_replica_of` from a
read replica to its `masterInstanceName`; and surfaces the private network,
CMEK key, and replica/master resource names as correlation anchors. The
sqladmin API commonly reports `masterInstanceName`/`replicaNames` entries as a
bare instance name with no project qualifier in the common same-project case
(rather than a `projects/<p>/instances/<i>` reference), so a bare name is
resolved against the instance's own project before the edge is built. The
`kmsKeyName` value is normalized the same way as other CMEK references: an
already CAI-prefixed (`//cloudkms.googleapis.com/...`) value is kept as-is,
and a bare value is prefixed only when it matches the expected
`projects/.../locations/.../keyRings/.../cryptoKeys/...` CryptoKey path shape,
so the anchor and edge target are never double-prefixed. No public or private
IP address (`ipAddresses[].ipAddress`) or authorized-network CIDR or label
(`settings.ipConfiguration.authorizedNetworks[].value`/`.name`) is ever
decoded — only the boolean `ipv4Enabled` posture and the authorized-network
entry count are kept, per the GCP collector contract Payload Boundaries.

**Cloud VPN Tunnel** (`compute.googleapis.com/VpnTunnel`) captures region, IKE
version, tunnel status, the HA/Classic gateway-interface indexes
(`vpnGatewayInterface`/`peerExternalGatewayInterface`), and bounded
local/remote traffic-selector counts; emits the typed
`vpn_tunnel_uses_vpn_gateway` edge for an HA VPN tunnel's own `vpnGateway`,
`vpn_tunnel_uses_target_vpn_gateway` for a Classic VPN tunnel's
`targetVpnGateway`, `vpn_tunnel_peers_with_vpn_gateway` for either an HA
peer-to-peer `peerGcpGateway` or an external `peerExternalGateway` (both peer
forms share one relationship type; the target's own asset type — `VpnGateway`
versus `ExternalVpnGateway` — distinguishes the topology), and
`vpn_tunnel_uses_router` to the Cloud Router used for BGP dynamic routing when
`router` is configured; and surfaces the resolved gateway, peer, and router
resource names as correlation anchors. This extractor reuses
`assetTypeComputeVPNGateway` (declared by the Cloud VPN Gateway extractor,
#4302) and `assetTypeComputeRouter` (declared by the Cloud Router extractor,
#4301), never redeclaring either, exactly as this extractor already reuses
`assetTypeComputeVpnTunnel` (declared by the Route extractor)
and `assetTypeComputeTargetVPNGateway` (declared by the ForwardingRule
extractor). The tunnel's own `peerIp`, `sharedSecret`, `sharedSecretHash`, and
`detailedStatus` fields are never decoded — no public or private IP address,
pre-shared-key material, or free-text status detail reaches a fact — and
`localTrafficSelector`/`remoteTrafficSelector` are reduced to bounded counts,
mirroring the Route extractor's destRange-to-prefix-length reduction; no CIDR
value ever leaves the parser.

Performance Evidence: this extractor adds no new hot path. It is a pure
in-process function over one already-parsed, bounded CAI `resource.data` JSON
blob (no loop over external data, no Cypher, no graph or Postgres write, no
worker/lease/queue, no live provider call); cost is O(1) per VPN tunnel asset.
No-Observability-Change: extraction outcomes are covered by the existing
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` counters; no new metric is needed.

The bounded `attributes` map surfaces through the cloud inventory readback
(`GET /api/v0/cloud/inventory`, `list_cloud_resource_inventory`) with truth
labels; `correlation_anchors` reach the canonical `CloudResource` graph node and
the typed edges materialize through `gcp_relationship_materialization` once both
endpoints resolve.

**Cloud Router** (`compute.googleapis.com/Router`) captures region, the
router-level BGP ASN and advertise mode, a bounded per-peer summary (name,
peer ASN, interface name) for every entry in `bgpPeers`, a bounded per-NAT
summary (name, NAT IP allocation option, source-subnetwork-ranges option) for
every entry in `nats`, a total peer count, a total NAT count, a total
interface count, the `encryptedInterconnectRouter` posture, and creation
time; emits the typed `router_in_network` edge to the enclosing Compute
`Network`, and for each entry in `interfaces` a `router_interface_linked_vpn_tunnel`
edge to a linked `VpnTunnel`, a `router_interface_linked_interconnect_attachment`
edge to a linked `InterconnectAttachment`, or a `router_interface_subnetwork`
edge to a linked `Subnetwork` (an interface names at most one of the three).
A BGP peer's `interfaceName` never becomes an edge endpoint on its own — only
the named interface's own linked resource is a resolvable CAI asset, so the
edge is derived from `interfaces`, not from `bgpPeers`. No BGP peer or
interface IP address (`bgpPeers[].ipAddress`/`.peerIpAddress`,
`interfaces[].ipRange`) and no NAT IP resource reference
(`nats[].natIps`/`.drainNatIps`) is ever decoded into an attribute or anchor,
per the GCP collector contract Payload Boundaries.

**Interconnect Attachment** (`compute.googleapis.com/InterconnectAttachment`)
captures region, attachment type (`DEDICATED`/`PARTNER`/`PARTNER_PROVIDER`/
`L2_DEDICATED`), provisioned bandwidth (the `BPS_*` enum), edge availability
domain, state, partner ASN, and creation time; emits the typed
`interconnect_attachment_uses_router` edge to the resolved Cloud `Router`,
`interconnect_attachment_uses_interconnect` edge to the resolved
`Interconnect`, and — only when `l2Forwarding` is present, i.e. for a
`type: L2_DEDICATED` attachment — `interconnect_attachment_uses_network` edge
to the VPC `Network` named by the nested `l2Forwarding.network`; and surfaces
each resolved resource name as a correlation anchor. The top-level
InterconnectAttachment resource carries no `network` field of its own, per the
live Compute v1 discovery document; only an L2_DEDICATED attachment's nested
`l2Forwarding.network` names an attached VPC network. This extractor reuses
`assetTypeComputeInterconnectAttachment` and `assetTypeComputeRouter` (both
declared by the Cloud Router extractor, #4301), never redeclaring either —
that extractor's own `router_interface_linked_interconnect_attachment` edge
already targets `assetTypeComputeInterconnectAttachment` (the attachment
itself, not the underlying Interconnect). This extractor declares
`assetTypeComputeInterconnect` here, fresh, for its own
`interconnect_attachment_uses_interconnect` edge target and for any future
Interconnect extractor to reuse. `l2Forwarding`'s own
`tunnelEndpointIpAddress` and `defaultApplianceIpAddress` fields, and its
per-VLAN-tag `applianceMappings`, are never decoded — every one resolves to an
IP address. `partnerAsn` is a string-encoded int64 per the compute API
convention; it is decoded via `json.RawMessage`/`parseFlexibleInt64` so an
absent value (the common case for a DEDICATED attachment, where the field is
not available) is distinguished from a legitimately present zero, rather than
fabricated. Every candidate/customer/cloud-router IP address field the
Compute API exposes on this resource (`candidateCloudRouterIpAddress`,
`candidateCustomerRouterIpAddress`, `cloudRouterIpAddress`,
`customerRouterIpAddress`, and their IPv6 counterparts, plus
`candidateSubnets` and `ipsecInternalAddresses`) is never decoded into Go
memory at all, per the GCP collector contract Payload Boundaries.

Performance Evidence: this extractor adds no new hot path. It is a pure
in-process function over one already-parsed, bounded CAI `resource.data` JSON
blob (no loop over external data, no Cypher, no graph or Postgres write, no
worker/lease/queue, no live provider call); cost is O(1) per Interconnect
Attachment asset.
No-Observability-Change: extraction outcomes are covered by the existing
`eshu_dp_gcp_cloud_attribute_extractions_total` and
`eshu_dp_gcp_cloud_facts_emitted_total` counters; no new metric is needed.

The bounded `attributes` map surfaces through the cloud inventory readback
(`GET /api/v0/cloud/inventory`, `list_cloud_resource_inventory`) with truth
labels; `correlation_anchors` reach the canonical `CloudResource` graph node and
the typed edges materialize through `gcp_relationship_materialization` once both
endpoints resolve.

**Cloud VPN Gateway** (`compute.googleapis.com/VpnGateway`) captures region,
stack type, gateway IP version, creation time, and a bounded VPN-interface
count; emits the typed `vpn_gateway_in_network` edge to the enclosing Compute
`Network`; and surfaces the network resource name as a correlation anchor.
VpnGateway is a regional-only asset type — GCP exposes no global variant,
unlike `ForwardingRule`/`Address` — and is distinct from
`compute.googleapis.com/TargetVpnGateway` (the older Classic VPN
target-gateway resource a `ForwardingRule.target` can reference, handled by
the Forwarding Rule extractor). Per-interface identity (`id`), the interface
`ipAddress`/`ipv6Address`, and any `interconnectAttachment` reference are
never decoded into Go memory at all — the interface struct declares no fields
for them — so only the interface count crosses the redaction boundary and no
public or private IP address reaches a fact.

**Memorystore Redis Instance** (`redis.googleapis.com/Instance`) captures
location id, Redis version, tier (`BASIC`/`STANDARD_HA`), memory size in GB,
connect mode (`DIRECT_PEERING`/`PRIVATE_SERVICE_ACCESS`), transit-encryption
mode, the boolean `authEnabled` posture, state, creation time, replica count,
read-replicas mode, the CMEK key name, and the persistence mode; emits the
typed `redis_instance_in_network` edge to the authorized Compute `Network`
(resolved from `authorizedNetwork` the same way the Cloud SQL Instance and VPC
Network extractors resolve a selfLink or project-qualified/project-less
partial) and the `redis_instance_encrypted_by_kms_key` edge to the CMEK
`CryptoKey` (an already CAI-prefixed `customerManagedKey` value is kept as-is;
a bare value is prefixed, mirroring the Dataproc Cluster and Cloud Storage
Bucket CMEK normalization); and surfaces the authorized network and CMEK key
resource names as correlation anchors. The Memorystore API's connection-plane
fields — `host`, `port`, `readEndpoint`, `readEndpointPort`,
`reservedIpRange`, and `secondaryIpRange` — are never decoded, since each is
an IP address, port, or CIDR range rather than a resource identity.

**Memorystore Memcached Instance** (`memcache.googleapis.com/Instance`)
captures display name, a bounded zone count, node count, per-node cpu count
and memory size in MB (from `nodeConfig`), the Memcached major version
(`memcacheVersion`) and full version string, creation time, state,
maintenance version, effective maintenance version, and a bounded
`memcacheNodes` count; emits the typed `memcache_instance_in_network` edge to
the authorized Compute `Network` (resolved from `authorizedNetwork` the same
way the Memorystore Redis Instance and VPC Network extractors resolve a
selfLink or project-qualified/project-less partial); and surfaces the
authorized network resource name as a correlation anchor. The Memcached API's
connection-plane fields — `discoveryEndpoint` and each `memcacheNodes[]`
entry's `host` and `port` — are never decoded, since each is a hostname, IP
address, or port rather than a resource identity; only `nodeId`, `zone`, and
`state` are declared on the per-node struct, and only their count crosses the
redaction boundary.

**Bigtable Instance** (`bigtableadmin.googleapis.com/Instance`) captures
display name, state, instance type (`PRODUCTION`/`DEVELOPMENT`), and edition.
The Bigtable Admin v2 Instance resource carries only instance-level metadata —
it has no clusters, encryption, or `kmsKeyName` field — so the Instance
extractor emits no outbound edges or correlation anchors. An Instance's own
labels are already captured by the shared envelope label path, so they are not
re-declared as a typed attribute.

**Bigtable Cluster** (`bigtableadmin.googleapis.com/Cluster`) is the separate
CAI asset type that carries a Bigtable instance's cluster topology. It captures
location id, state, serve nodes, node scaling factor, default storage type, and
the CMEK key name from `encryptionConfig.kmsKeyName`; emits a
`bigtable_cluster_in_instance` edge to the parent Instance (derived from the
cluster's own resource name, which embeds its parent as
`.../instances/<instance>/clusters/<cluster>`) and a
`bigtable_cluster_encrypted_by_kms_key` edge to the CMEK `CryptoKey` (an
already CAI-prefixed value is kept as-is, a bare value is prefixed, mirroring
the Memorystore Redis Instance CMEK normalization); and surfaces the parent
Instance and CMEK key resource names as correlation anchors. Table schemas and
row data are data-plane content and are never decoded.

**Dataflow Job** (`dataflow.googleapis.com/Job`) captures job type
(`JOB_TYPE_BATCH`/`JOB_TYPE_STREAMING`), current state, location, create/start
time, and the SDK version plus `sdkSupportStatus` lifecycle enum
(`UNKNOWN`/`SUPPORTED`/`STALE`/`DEPRECATED`/`UNSUPPORTED`) reported under
`jobMetadata.sdkVersion`; emits the typed `dataflow_job_uses_network` and
`dataflow_job_uses_subnetwork` edges resolved from the first
`environment.workerPools` entry that reports a network or subnetwork reference,
with both endpoints taken from that same single pool so a network from one pool
and a subnetwork from a different pool are never paired into a placement that
never co-occurred on any real worker pool (a bare short name is promoted to the
project-less global/regional partial and resolved the same way the GKE Cluster
and Dataproc Cluster extractors resolve their own network/subnetwork
references, with a bare subnetwork name resolved against the worker pool's own
zone), the `dataflow_job_uses_staging_bucket` edge to the GCS bucket parsed
from `environment.tempStoragePrefix` — which the Dataflow API documents in the
resource forms `storage.googleapis.com/{bucket}/{object}` and
`{bucket}.storage.googleapis.com/{object}` (a `gs://bucket/object` value is
also accepted defensively), all reduced to the bucket identity so the
temp-location object path is dropped as a data-plane locator — and the
`dataflow_job_encrypted_by_kms_key` edge to the CMEK `CryptoKey` from
`environment.serviceKmsKeyName` (normalized the same way the Dataproc Cluster
and Memorystore Redis Instance CMEK edges handle an already-prefixed or bare
key name); and surfaces the fingerprinted runtime worker service-account email
(`environment.serviceAccountEmail`) plus the network, subnetwork, staging
bucket, and CMEK key resource names as correlation anchors, never the raw
service-account address. No pipeline parameter value, `environment.userAgent`,
`environment.sdkPipelineOptions` option value, or step-graph content is ever
decoded, since these can carry operator-supplied values unrelated to resource
identity.

**Workflows Workflow** (`workflows.googleapis.com/Workflow`) captures
deployment `state` (`STATE_UNSPECIFIED`/`ACTIVE`/`UNAVAILABLE`), revision id,
`callLogLevel`, `executionHistoryLevel`, create/update/revision-create time,
the normalized CMEK key relative name, a `sourceContents` presence flag, and
the fingerprinted runtime service-account email — verified against the live
Workflows v1 discovery document; emits the typed `workflow_encrypted_by_kms_key`
edge to the CMEK `CryptoKey` when `cryptoKeyName` resolves to a valid CryptoKey
full name (an already-prefixed or bare key name is normalized the same way the
Dataflow Job and Memorystore Redis Instance CMEK edges handle it, and a
leading `/` is trimmed, mirroring the Filestore Instance and Dataflow Job CMEK
helpers) and surfaces the CMEK key resource name plus the service-account
fingerprint as correlation anchors. The Workflows v1 API documents two
project-inferred `cryptoKeyName` forms — a `"projects/-/..."` wildcard and a
project-less `"locations/..."` form, both meaning "infer the project from the
workflow's own project" — both are qualified against the workflow's own
`ctx.ProjectID` rather than producing an edge rooted at the literal
`projects/-` segment or silently dropping the relationship. The
`crypto_key_name` attribute and the CMEK edge/anchor are set only together,
from the same resolved full name, so an unnormalizable or unqualifiable
`cryptoKeyName` (a malformed shape, a wrong-domain already-prefixed value, or
a project-inferred form with no project to qualify against) is dropped
entirely rather than surfaced unresolved. The runtime service account is never
emitted as an edge — the same treatment as the Dataflow Job, Dataproc Cluster,
and GKE Cluster extractors' own service accounts — since an email is not an
exactly resolvable CAI endpoint. `sourceContents` (the workflow's YAML/JSON
definition body, capped at 128KB by the API) is decoded only far enough to set
a boolean presence flag, and the decoded copy is cleared immediately
afterward: no step, argument, header, or embedded credential value from that
body is ever read, so a called service (Cloud Run, Cloud Functions, or an
arbitrary HTTP endpoint) referenced only inside the workflow definition is out
of reach of this safe-metadata extractor and is not modeled as a graph edge.
`userEnvVars` and `tags` are not decoded in typed depth; workflow `labels`
continue to flow through the collector's shared label/tag path, which already
captures and fingerprints them.

**Org Policy** (`orgpolicy.googleapis.com/Policy`) captures the constraint
name, a bounded rule-shape summary of the spec's `rules[]` (total rule count
plus per-kind counts for allow-values/deny-values/allow-all/deny-all/
condition-present rules, and a count of rules that enforce), the spec's
`inheritFromParent` and `reset` booleans, a fingerprinted spec etag, the spec
`updateTime`, and dry-run-spec presence with its own bounded rule count. The
constraint name and the bound organization/folder/project target are both
derived from the Policy's own CAI full resource name —
`//orgpolicy.googleapis.com/{organizations|folders|projects}/<id>/policies/<constraint>`,
per the Cloud Asset Inventory resource-name-format reference — never from
`resource.data.name`, which is untrusted parser input. The derivation fails
closed: it requires the exact `//orgpolicy.googleapis.com/` prefix and a
`<kind>/<id>/policies/<constraint>` shape, so a relative or wrong-service name
mints no edge or anchor. On success it emits a single
`org_policy_applies_to_resource` edge to the resolved
`cloudresourcemanager.googleapis.com/Organization`, `.../Folder`, or
`.../Project` node. The rule union's allowed/denied VALUE lists (each entry can
be an organization-specific resource identifier, project id, or other value),
the condition CEL expression text, and any custom-constraint `parameters` (a
`google.protobuf.Struct`) are decoded only to compute bounded counts and are
never persisted or surfaced to any fact — only bounded counts and booleans
leave the parser, mirroring the Custom IAM Role extractor's treatment of its
permission list.

**Network Endpoint Group** (`compute.googleapis.com/NetworkEndpointGroup`)
captures `networkEndpointType` (decoded as a free string, never validated
against a hardcoded enum, since the Compute API is the source of truth —
verified against the live Compute v1 discovery document, whose enum is
`GCE_VM_IP`, `GCE_VM_IP_PORT`, `GCE_VM_IP_PORTMAP`, `INTERNET_FQDN_PORT`,
`INTERNET_IP_PORT`, `NON_GCP_PRIVATE_IP_PORT`, `PRIVATE_SERVICE_CONNECT`,
`SERVERLESS`), size, default port, zone or region placement, creation time, a
serverless discriminator plus service/function name for a `SERVERLESS` NEG's
exactly one configured `cloudRun`/`appEngine`/`cloudFunction` backend,
Private Service Connect posture (connection status, producer port, the raw
connection id string, and a fingerprinted target-service hostname), and a
bounded annotation count. Emits `network_endpoint_group_in_network` and
`network_endpoint_group_in_subnetwork` edges to the resolved Network and
Subnetwork; emits no edge back to the enclosing BackendService, since that
relationship is already emitted, in the opposite direction, by the Backend
Service extractor's shared `backend_service_has_backend` edge (which already
resolves toward this same `compute.googleapis.com/NetworkEndpointGroup` asset
type as its group-kind target). The serverless discriminator is set from
sub-object presence, so a URL-mask NEG with no fixed service name still
classifies; a `cloudRun.service` fixed name additionally emits a
`network_endpoint_group_targets_serverless_service` edge to the
`run.googleapis.com/Service` resolved in the NEG's own project and region
(mirroring the Eventarc Trigger Cloud Run edge). An `appEngine`/`cloudFunction`
reference stays a bounded attribute only, never an edge, because an App Engine
app id need not equal the project id and a Cloud Function reference carries no
gen1/gen2 or region qualifier, so neither resolves to an exact CAI endpoint
from the NEG alone; the `urlMask`/`tag`/`version` fields are data-plane routing
templates and are never decoded into an attribute or anchor.
`pscTargetService` is resolved two mutually-exclusive ways: a Producer Service
Attachment self-link emits a `network_endpoint_group_targets_service_attachment`
edge to the `compute.googleapis.com/ServiceAttachment` (the same asset type the
ForwardingRule extractor resolves), while a bare Google API hostname names no
resolvable CAI resource and is reduced to a deterministic host fingerprint
mirroring the Pub/Sub Subscription push-endpoint host-fingerprint treatment;
a PSC NEG's `pscData.consumerPscAddress` (the allocated VIP) is never decoded
into an attribute or anchor at all, and `pscConnectionId` is kept as the raw
string the API reports rather than parsed to a numeric type, since it is a
Compute-assigned uint64 that can exceed int64/float64 precision.
`annotations` is a label-shaped map; only its bounded count is surfaced,
mirroring the Filestore Instance and Workflows Workflow treatment of
labels/tags already captured by the collector's shared label path.

**Cloud Armor Security Policy** (`compute.googleapis.com/SecurityPolicy`)
captures the policy type (`CLOUD_ARMOR`, `CLOUD_ARMOR_EDGE`, or
`CLOUD_ARMOR_NETWORK` per the Compute SecurityPolicy schema), region (present
only for a regional policy — a global policy reports no region field), a
bounded per-rule summary of priority, action, and preview (non-enforced)
posture plus a rule count, the Cloud Armor Adaptive Protection layer-7 DDoS
defense enabled posture, and creation time. Priority is parsed with the same
absent/null-versus-present-zero handling as the Firewall and Route
extractors' priority fields, since the Compute SecurityPolicyRule schema
defines priority as a positive value between 0 and 2147483647 where 0 is the
legitimate highest-priority rule, not an absent-field sentinel; an absent or
null priority is omitted rather than fabricated as 0, and a present priority
of 0 is kept. Preview is a pointer so a present `false` (an enforced rule) is
distinguishable from an absent field. The policy's graph edge is inbound: a
Backend Service references it through its own
`securityPolicy`/`edgeSecurityPolicy` fields and resolves the
`backend_service_uses_security_policy` / `backend_service_uses_edge_security_policy`
edge from that side (the Backend Service extractor), the same inbound-only
edge shape as the Custom IAM Role and SSL Certificate extractors, so this
extractor emits no outbound edge or anchor of its own. A rule's match
condition (`match`/`networkMatch`, including any `srcIpRanges`/CIDR values),
rate-limit and redirect configuration, and free-text description are never
decoded — only the rule's priority, its action string (a small
Google-controlled vocabulary such as `allow`, `deny(403)`, `rate_based_ban`,
never user-supplied match data), and its preview posture leave the parser.

**API Gateway Gateway** (`apigateway.googleapis.com/Gateway`) captures display
name, lifecycle state, region (derived from the Gateway's own CAI full
resource name — `.../locations/<region>/gateways/<gateway>` — since the API
Gateway v1 Gateway resource carries no separate region field of its own),
creation and update time, and a fingerprint of the `defaultHostname` live DNS
name (`{gatewayId}-{hash}.{region_code}.gateway.dev`); emits the typed
`api_gateway_uses_api_config` edge to the resolved
`apigateway.googleapis.com/ApiConfig` (a separately CAI-inventoried asset
type, verified against the live Cloud Asset Inventory supported-asset-types
reference); and surfaces the ApiConfig resource name as a correlation anchor.
The `apiConfig` reference is untrusted parser input, so the derivation fails
closed: an already-absolute value is trusted only when it carries the exact
`//apigateway.googleapis.com/` CAI service prefix, and a relative value is
promoted to a full resource name only when it matches the documented
`projects/{project}/locations/global/apis/{api}/configs/{apiConfig}` shape; any
other value mints no edge or anchor. The Gateway v1 REST resource
(`projects.locations.gateways`) reports exactly
this field set — `name`, `createTime`, `updateTime`, `labels`, `displayName`,
`apiConfig`, `state`, `defaultHostname` — with no additional field to decode;
the raw `defaultHostname` DNS name is never persisted, only its deterministic
fingerprint, mirroring the Pub/Sub Subscription push-endpoint host treatment.

The bounded `attributes` map surfaces through the cloud inventory readback
(`GET /api/v0/cloud/inventory`, `list_cloud_resource_inventory`) with truth
labels; `correlation_anchors` reach the canonical `CloudResource` graph node and
the typed edges materialize through `gcp_relationship_materialization` once both
endpoints resolve.

**Composer Environment** (`composer.googleapis.com/Environment`) captures
lifecycle state, creation time, environment size, resilience mode, the Airflow
`softwareConfig.imageVersion`, CMEK posture, private-environment and
private-GKE-endpoint posture, the private networking connection type, a
workloads-config presence flag, and the fingerprinted node-runtime service
account email; emits the typed `composer_environment_uses_gke_cluster` edge to
the environment's own GKE `Cluster` (`config.gkeCluster`, always a relative
resource name per the Composer API), `composer_environment_uses_network` /
`composer_environment_uses_subnetwork` edges to `config.nodeConfig.network` /
`.subnetwork` (a bare network short name is promoted to the project-less
global partial and resolved the same way the GKE Cluster and Dataproc Cluster
extractors resolve their own network reference; the subnetwork is always a
relative resource name per the Composer API, unlike GKE or Dataproc, which
also accept a bare subnetwork short name),
`composer_environment_encrypted_by_kms_key` to the CMEK `CryptoKey` from
`config.encryptionConfig.kmsKeyName` (normalized the same way the Dataproc
Cluster CMEK edge handles an already-prefixed or bare key name), and
`composer_environment_uses_dag_bucket` to the DAG/data Cloud Storage bucket
parsed from `config.dagGcsPrefix` (`gs://{bucket}/dags`, present across every
Composer generation) with `storageConfig.bucket` (no `gs://` prefix, Composer
3+ only) as a fallback when `dagGcsPrefix` is absent; and surfaces the GKE
cluster, network, subnetwork, CMEK key, and fingerprinted service-account
email as correlation anchors. The node-runtime service account is never
emitted as an edge — the same treatment as the Dataproc Cluster and GKE
Cluster extractors' own service accounts — since an email is not an exactly
resolvable CAI endpoint, and the GKE "default" service-account sentinel is
never fingerprinted or anchored, mirroring the GKE Cluster extractor's own
sentinel handling. Per-key Airflow `softwareConfig.airflowConfigOverrides` and
`envVariables` values, `maintenanceWindow` recurrence, and
`privateEnvironmentConfig.privateClusterConfig.masterIpv4CidrBlock` /
`ipAllocationPolicy` CIDR values are never decoded into an attribute or
anchor; `workloadsConfig`'s per-component CPU/memory/storage sizing is decoded
only far enough to set a boolean presence flag.

**AlloyDB Cluster** (`alloydb.googleapis.com/Cluster`) captures display name,
uid, state, cluster type (`PRIMARY`/`SECONDARY`), database version,
subscription type, creation time, the CMEK KMS key name, the encryption type
posture (`GOOGLE_DEFAULT_ENCRYPTION`/`CUSTOMER_MANAGED_ENCRYPTION`), and the
automated/continuous backup posture — automated backup enabled, its location
and backup window, its time-based retention period or quantity-based
retention count (whichever the policy configures), and continuous-backup
enabled plus its point-in-time-recovery window in days. It emits the typed
`alloydb_cluster_in_network` edge to the private Compute `Network` referenced
by `networkConfig.network` (falling back to the deprecated top-level
`network` field only when `networkConfig` carries none, per the AlloyDB v1
discovery document's deprecation note) and the
`alloydb_cluster_encrypted_by_kms_key` edge to the CMEK `CryptoKey`, surfacing
both resource names as correlation anchors. The network reference's project
segment is carried through exactly as AlloyDB reports it and is never
rewritten to the cluster's own project id: AlloyDB supports Shared VPC, where
a cluster's own project (a Shared VPC service project) can reference a
network owned by a different host project, so a numeric project segment
cannot be assumed to be the cluster's own project number. A numeric-project
reference that does not match any collected Compute Network's
project-id-keyed CAI identity simply does not resolve to an edge — the safe
outcome — rather than risking a rewritten, fabricated edge to a same-named
network that happens to exist in the cluster's own project. `initialUser`
(the input-only database username/password pair used at cluster-creation
time) is never decoded — no field of it reaches the extractor's input struct
— so no database credential can ever reach the output;
`encryptionInfo.kmsKeyVersions` is left undecoded for the same reason
`EncryptionInfo`'s other data-plane-adjacent fields are: it is a key-version
identifier list, not a control-plane field useful for Terraform, drift, or
correlation. An AlloyDB Instance (`alloydb.googleapis.com/Instance`) is a
separate CAI asset type not covered by this extractor; the cluster-to-instance
edge will resolve from the Instance side once an Instance extractor is added,
the same inbound-edge pattern the Bigtable Cluster/Instance pair and KMS
KeyRing/CryptoKey pair already use in this package.

## Consumption Decisions

Every emitted GCP cloud fact family and every GCP Secrets/IAM mirror fact emitted
by this collector has an explicit consume-or-provenance decision. Changing a
provenance-only family into platform truth requires a reducer or read-model
design and a matching test update.

| Fact kind | Decision | Consumer or owner |
| --- | --- | --- |
| `gcp_cloud_resource` | consumed | Cloud inventory, runtime drift, and `gcp_resource_materialization` reducers. |
| `gcp_collection_warning` | provenance/audit only | Fact-store audit evidence plus collector telemetry counters. |
| `gcp_cloud_relationship` | consumed | `gcp_relationship_materialization` reducer after both endpoints resolve exactly; cross-repo relationship resolver only when supported source and target full resource names each match one distinct repository catalog entry. |
| `gcp_tag_observation` | consumed | Cloud tag evidence loader and cloud inventory readback; tags never admit resources by themselves. |
| `gcp_iam_policy_observation` | provenance/audit only | Raw IAM snapshot evidence; secrets/IAM reducers consume derived GCP IAM source facts. |
| `gcp_iam_principal` | consumed | `secrets_iam_trust_chain` reducer. |
| `gcp_iam_trust_policy` | consumed | `secrets_iam_trust_chain` reducer. |
| `gcp_iam_permission_policy` | consumed | `secrets_iam_trust_chain` reducer. |
| `gcp_dns_record` | provenance/audit only | Redaction-safe DNS evidence until a DNS read model or resolver admits it. |
| `gcp_image_reference` | consumed | `container_image_identity` reducer with digest-first or explicit tag-confidence behavior. |

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
- For ServiceAccount impersonation trust facts, do not persist the raw target
  service-account email, workload pool, namespace, Kubernetes ServiceAccount
  name, or IAM member string. Use the shared email digest, member fingerprint,
  and Workload Identity subject fingerprint.
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
4. IAM reducers treat `gcp_iam_policy_observation` as provenance-only policy
   evidence. Principal fingerprints do not become user nodes unless a later
   identity design admits them. Secrets/IAM correlation consumes the derived
   `gcp_iam_principal`, `gcp_iam_permission_policy`, and
   `gcp_iam_trust_policy` source facts instead of the raw IAM snapshot.
5. Relationship reducers materialize graph edges only when both endpoints
   resolve exactly in the current allowed scope. Cross-project, cross-folder,
   missing, unsupported, ambiguous, and stale endpoints are counted and
   surfaced, never fabricated.
6. Image-reference reducers require digest-first or otherwise explicit
   tag-confidence behavior before using GCP image evidence in deployment or
   vulnerability paths.
7. `gcp_dns_record` remains redaction-safe DNS provenance until a DNS read model
   or resolver contract is implemented. It must not mint graph, service, or
   routing truth by itself.
8. `gcp_collection_warning` remains coverage/audit evidence. Warning facts can
   explain partial collection and telemetry counts, but they do not admit
   inventory, DNS, IAM, or relationship truth.

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
| Typed-depth extraction | A registered asset-type extractor (BigQuery Table, BigQuery Dataset, Subnetwork, Artifact Registry DockerImage, VPC Network, Forwarding Rule, IAM Service Account, Persistent Disk, Secret Manager Secret, Custom IAM Role, Pub/Sub Topic, Cloud Run Service, Pub/Sub Subscription, Cloud Run Revision, IAM Service Account Key, Firestore Database, IAM Workload Identity Pool, IAM Workload Identity Pool Provider, Dataproc Cluster, Cloud Functions Function, Secret Manager Secret Version, BigQuery Routine, API Key, Cloud Functions (gen1), Dataform Repository, reCAPTCHA Enterprise Key, BigQuery Data Transfer Config, Cloud Build Build, Cloud Build Trigger, Identity Platform Config, Dataplex Entry Group, Artifact Registry Repository, Logging Log Bucket, Eventarc Trigger, Cloud Scheduler Job, Logging Log Sink, Cloud Tasks Queue, Firebase Project, App Engine Service, App Engine Application, Firebase App Info, Firebase Rules Ruleset, Cloud DNS Managed Zone, Cloud DNS Policy, Cloud Storage Bucket, KMS CryptoKey, KMS KeyRing, GKE Cluster, Cloud SQL Instance, Backend Service, Cloud Router, Memorystore Redis Instance, Health Check, SSL Certificate, Target HTTPS Proxy, Bigtable Instance, Bigtable Cluster, Dataflow Job, Filestore Instance, Workflows Workflow, Org Policy, Network Endpoint Group, Cloud Armor Security Policy, API Gateway Gateway, Interconnect Attachment, Composer Environment, Certificate Manager Certificate, AlloyDB Cluster) produces a bounded `attributes` map, `correlation_anchors`, and typed edges from `resource.data`; the raw blob never leaves the parser, external object paths are dropped, no public/private IP address, CIDR, or authorized-network label is persisted (subnet ranges are reduced to a prefix length; a Forwarding Rule's reserved IP address is never decoded at all; Cloud SQL IP posture is reduced to the `ipv4Enabled` boolean plus an authorized-network count; a Cloud Router's BGP peer/interface IP addresses and NAT IP resource references are never decoded at all, only bounded name/ASN/option summaries and resolvable network/tunnel/attachment/subnetwork edges are kept; Memorystore's host, port, read-endpoint, and IP-range fields are never decoded at all; a Filestore Instance's `networks[].reservedIpRange` is never decoded at all), KMS references are reduced to the CryptoKey resource name with no key material, no secret payload is persisted, Cloud Run env values are never read (only env keys and control-plane references) with runtime service-account emails reduced to a fingerprint, Pub/Sub push endpoints are reduced to scheme plus a host fingerprint with paths and query dropped, no service-account private/public key material is persisted, Workload Identity provider OIDC JWKS/SAML metadata and attribute-mapping/condition expressions are never persisted (only the external trust anchor, mapping key count, and a condition-presence flag), Firebase rule source content is never read (only the source file count and target-service enum are kept), the Managed Zone's own DNS name and forwarding target IPs/hostnames are never decoded into an attribute or anchor (only bounded posture and resolvable VPC network edges are kept), a DNS Policy's own `description` and alternative-name-server addresses are never decoded into an attribute or anchor (only bounded posture, a bounded alternative-name-server count, and resolvable VPC network edges are kept), an SSL Certificate's managed domains, subject alternative names, and self-managed certificate/private-key material are never decoded into an attribute or anchor (only bounded domain and SAN counts are kept), a Filestore Instance's per-file-share name/capacity is reduced to a bounded count while its labels surface only as a bounded `label_count` in typed depth (the labels themselves are still captured and value-fingerprinted per `redaction_policy_version` through the collector's shared label path), an Org Policy's rule `values.allowedValues`/`deniedValues` entries, condition CEL expression text, and custom-constraint `parameters` Struct are decoded only to compute bounded per-kind rule counts and are never persisted or surfaced to any attribute or anchor, a Network Endpoint Group's PSC `consumerPscAddress` VIP is never decoded at all while its serverless `cloudRun`/`appEngine`/`cloudFunction` `urlMask`/`tag`/`version` routing templates are never decoded into an attribute or anchor (a Cloud Run service and a PSC Producer Service Attachment self-link resolve to typed edges; a bare PSC Google-API-hostname target is reduced to a host fingerprint), a Cloud Armor Security Policy's rule `match`/`networkMatch` condition (including any `srcIpRanges`/CIDR values), rate-limit/redirect configuration, and free-text description are never decoded — only a bounded per-rule priority/action/preview summary and rule count are kept (an absent or null priority is omitted rather than fabricated as 0, since 0 is the schema's legitimate highest-priority value), an API Gateway Gateway's `defaultHostname` live DNS name is never persisted, only its deterministic fingerprint (mirroring the Pub/Sub Subscription push-endpoint host treatment), an Interconnect Attachment's `candidateCloudRouterIpAddress`/`candidateCustomerRouterIpAddress`/`cloudRouterIpAddress`/`customerRouterIpAddress` fields (and their IPv6 counterparts), `candidateSubnets`, `ipsecInternalAddresses`, and (for an L2_DEDICATED attachment) its `l2Forwarding.tunnelEndpointIpAddress`/`.defaultApplianceIpAddress`/`.applianceMappings` are never decoded at all — only bounded region/type/bandwidth/state posture, partner ASN, and resolvable Router/Interconnect/Network edges are kept, a Composer Environment's `privateEnvironmentConfig.privateClusterConfig.masterIpv4CidrBlock`, any `ipAllocationPolicy` field, and per-key `softwareConfig.airflowConfigOverrides`/`envVariables` values are never decoded at all — only bounded lifecycle/size/resilience/networking posture, the Airflow image version, and resolvable GKE cluster/network/subnetwork/CMEK-key/DAG-bucket edges are kept, a Certificate Manager Certificate's managed domains, subject alternative names, managed-identity SPIFFE ID, and certificate/private-key material are never decoded into an attribute or anchor (only bounded domain, DNS-authorization, and SAN counts are kept), and an AlloyDB Cluster's `initialUser` username/password pair is never decoded at all (no field of it reaches the extractor's input struct), so no database credential can ever reach the output; its network reference's project segment is likewise never rewritten to the cluster's own project id, since AlloyDB's Shared VPC support means that segment can legitimately name a different host project, and a Cloud Build Trigger's `build` template, `substitutions`, free-text CEL `filter`, `webhookConfig.secret`, GitHub/GitLab/Bitbucket push/pull-request branch/tag regex detail, `gitFileSource`, and `sourceToBuild.uri` are never decoded — only a bounded control-plane posture, a derived `source_type` enum reflecting the trigger's true firing mechanism (never shadowed by a coexisting `sourceToBuild`), and the resolvable Cloud Source Repositories / Developer Connect GitRepositoryLink edges are kept. The `attributes` map surfaces through the cloud inventory readback with truth labels. |
| Direct API fallback | Fallback only runs for allowlisted families and emits separate warning evidence when skipped. |
| Reducer truth | Exact, derived, partial, stale, unavailable, and unsupported GCP paths agree across reducer facts and API/MCP reads. |

## Implementation Order

Implemented slices:

1. Fact constants, schema helpers, fixture payload tests, CAI parsing, and
   redacted source fact emission.
2. Fixture-backed `gcpruntime.Source` and `cmd/collector-gcp-cloud` runtime
   scaffolding through the `PageProvider` seam.
3. Shared cloud inventory admission/API/MCP readback for
   `gcp_cloud_resource`.
4. Tag evidence admission, image identity admission, relationship resolution,
   and GCP IAM trust facts.
5. Explicit-injection `gcpruntime.LiveClient` REST transport for bounded
   `assets.list` page reads.
6. Claimed-live command wiring, scheduler planning, default-off Helm exposure
   with ServiceMonitor coverage, and opt-in direct/effective tag API evidence.

Remaining gated slices:

1. Sanitized live smoke proof.

Observability change: the first slice adds the `gcp_cloud_resource`,
`gcp_cloud_relationship`, `gcp_tag_observation`,
`gcp_iam_policy_observation`, `gcp_dns_record`, `gcp_image_reference`, and
`gcp_collection_warning` fact schemas and the scoped GCP collector telemetry
series listed under
[Telemetry](#telemetry)
(`eshu_dp_gcp_cloud_*`). The runtime-scaffolding slice adds the fixture-backed
binary, explicit-injection live transport, claimed-live command path, and
default-off Helm exposure described below.

## Runtime Scaffolding Evidence

This evidence covers the second slice plus the default-off `LiveClient` transport
adapter: the `gcpruntime` source and the `collector-gcp-cloud` binary. These are
hot-path collector files, so they carry tracked performance and observability
evidence here.

Collector Performance Evidence: see the No-Regression Evidence block below.

Collector Observability Evidence: see the Observability Evidence block below.

Collector Deployment Evidence: the chart renders no GCP Deployment by default.
When `gcpCloudCollector.enabled=true`, it starts `/usr/local/bin/eshu-collector-gcp-cloud -mode claimed-live`, requires active workflow claims plus a matching live-enabled `gcp` instance, mounts the redaction key from a read-only Secret file, uses pod identity for GCP credentials, and renders metrics Service, ServiceMonitor, NetworkPolicy, and PodDisruptionBudget coverage.

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
contract. `LiveClient` records no additional telemetry by itself; provider
warnings flow through the existing warning metric after `Source` converts them
to `gcp_collection_warning` facts.

No-Regression Evidence:

- Baseline: before this slice there was no GCP collector runtime, and before the
  live-adapter slice `LiveClient` was an inert stub. The comparison is against
  the empty/no-runtime baseline, the fixture-backed source shape, and the inert
  live seam.
- After: `go test ./internal/collector/gcpcloud/gcpruntime/...
  ./cmd/collector-gcp-cloud/... ./internal/collector/gcpcloud/...
  ./internal/facts/... -count=1` passes. `go test
  ./internal/collector/gcpcloud/gcpruntime -run
  'TestLiveClient|TestSourceProviderWarning' -count=1` proves the live adapter.
- Backend/version: no graph or database backend is exercised. The source builds
  facts in memory through the fixture `PageProvider`; the binary commits through
  the existing `postgres.IngestionStore` unchanged by this slice.
- Input shape: two-page `assets.list` fixtures (three resources) per scope, plus
  stale-generation, dangling-page-token, multi-scope, empty-scope, local HTTP
  live-adapter pagination, path-validation, OAuth-scope, asset-family filter,
  retry, quota, permission, and unavailable-provider cases.
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
  wires no live provider by default, and adds no new database query, Cypher,
  lease, or concurrent writer. Pagination resumes strictly by continuation
  token, so an expired token or typed live provider warning degrades to a durable
  coverage warning rather than truncation.

Observability Evidence:

- The source records the existing scoped `eshu_dp_gcp_cloud_*` instruments
  (claims, pages, page-token resumes, facts emitted, warnings, freshness lag)
  through `gcpcloud.Metrics`. Every metric label is a bounded enum: collector
  kind, claim status, parent scope kind, fact kind, warning kind, and outcome.
- The status committer records `eshu_dp_gcp_cloud_claims_total` with
  `status=succeeded` or `status=failed` on commit outcome.
- One structured log line per committed scope reports bounded counts only
  (page, resource, and warning counts plus bounded parent scope and family
  enums). No instrument or log field carries a resource name, project id,
  derived scope id, label value, IAM member, DNS name, image reference, URL,
  credential name, page token, or provider response body. Credentials are
  referenced by name only and the redaction key is never logged.
