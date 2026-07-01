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
kept only as an attribute.

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

**App Engine Service** (`appengine.googleapis.com/Service`) captures the service
ID and traffic-split posture: the shard strategy (`split_shard_by`), the count
of version allocations (`version_count`), and the per-version allocation map
(`traffic_allocations`, version id → percentage). Version IDs are control-plane
identifiers and are safe to store verbatim. For each allocation key the extractor
emits one typed `service_splits_traffic_to_version` edge to the corresponding
`appengine.googleapis.com/Version` resource, whose full resource name is derived
by appending `/versions/{versionID}` to the service full resource name; the
version full resource names are also surfaced as correlation anchors. The raw
`name` field, service configuration (env, scaling, handlers), and data-plane
content are not decoded.

The bounded `attributes` map surfaces through the cloud inventory readback
(`GET /api/v0/cloud/inventory`, `list_cloud_resource_inventory`) with truth
labels; `correlation_anchors` reach the canonical `CloudResource` graph node and
the typed edges materialize through `gcp_relationship_materialization` once both
endpoints resolve.

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
| Typed-depth extraction | A registered asset-type extractor (BigQuery Table, BigQuery Dataset, Subnetwork, Artifact Registry DockerImage, VPC Network, IAM Service Account, Persistent Disk, Secret Manager Secret, Custom IAM Role, Pub/Sub Topic, Cloud Run Service, Pub/Sub Subscription, Cloud Run Revision, IAM Service Account Key, Firestore Database, IAM Workload Identity Pool, IAM Workload Identity Pool Provider, Dataproc Cluster, Cloud Functions Function, Secret Manager Secret Version, BigQuery Routine, API Key, Cloud Functions (gen1), Dataform Repository, reCAPTCHA Enterprise Key, BigQuery Data Transfer Config, Cloud Build Build, Identity Platform Config, Dataplex Entry Group, Artifact Registry Repository, Logging Log Bucket, Eventarc Trigger, Cloud Scheduler Job, Logging Log Sink, App Engine Service) produces a bounded `attributes` map, `correlation_anchors`, and typed edges from `resource.data`; the raw blob never leaves the parser, external object paths are dropped, no public/private IP address or CIDR is persisted (subnet ranges are reduced to a prefix length), KMS references are reduced to the CryptoKey resource name with no key material, no secret payload is persisted, Cloud Run env values are never read (only env keys and control-plane references) with runtime service-account emails reduced to a fingerprint, Pub/Sub push endpoints are reduced to scheme plus a host fingerprint with paths and query dropped, no service-account private/public key material is persisted, and Workload Identity provider OIDC JWKS/SAML metadata and attribute-mapping/condition expressions are never persisted (only the external trust anchor, mapping key count, and a condition-presence flag). The `attributes` map surfaces through the cloud inventory readback with truth labels. |
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
