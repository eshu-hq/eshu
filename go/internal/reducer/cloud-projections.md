# Reducer Cloud Domain Projections

Split from `README.md` (issue #5786). GCP, multi-cloud, secrets/IAM,
S3, RDS, EC2, and PagerDuty projection detail lives here; keep the
package overview in `README.md`.

## GCP Cloud Resource Materialization

`DomainGCPResourceMaterialization` mirrors `DomainAWSResourceMaterialization`
for GCP. It loads the scope generation's `gcp_cloud_resource` facts, projects
them into deterministic `CloudResource` node rows keyed by
`cloudResourceUID(project_id, location, asset_type, full_resource_name)`, and
writes them through the provider-neutral `CloudResourceNodeWriter` (no new
Cypher). Incomplete identities (missing `full_resource_name` or `asset_type`)
are dropped, never fabricated; duplicate facts converge on one node by uid. The
globally-unique Cloud Asset Inventory `full_resource_name` is stored as
`resource_id` so the GCP relationship edge projection (#2348) can resolve
endpoints exactly by name. After the node write succeeds (or is a legitimate
no-op for an empty generation) the handler publishes the
`cloud_resource_uid` `canonical_nodes_committed` readiness phase under the
distinct `gcp_resource_materialization:<scope>` acceptance unit, so the GCP edge
stage gates on GCP node readiness without colliding with the AWS node phase.

No-Regression Evidence: `go test ./internal/reducer -run
'GCPResourceMaterialization|ExtractGCPCloudResourceNodeRows' -count=1` (handler,
node extraction, dedupe, identity-drop, and phase-publish behavior) plus
`go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractGCPCloudResourceNodeRows' -benchmem`: GCP node extraction of
5,000 `gcp_cloud_resource` facts measured 13.0 ms/op vs the proven AWS
`BenchmarkExtractCloudResourceNodeRows` at 17.7 ms/op on the same host — the
bounded O(R) extraction carries the same shape as the AWS substrate, and the
graph write reuses the unchanged `canonicalCloudResourceUpsertCypher` writer, so
no new hot-path Cypher is introduced.

Observability Evidence: the handler emits a `gcp resource materialization
completed` structured completion log carrying `scope_id`, `generation_id`,
`domain`, `fact_count`, `node_count`, and per-stage
`load_facts_duration_seconds` / `extract_duration_seconds` /
`graph_write_duration_seconds` / `phase_publish_duration_seconds` /
`total_duration_seconds`, so an operator can see whether GCP node materialization
is fact-load, extraction, or graph-write bound at 3 AM.

## GCP Relationship Edge Materialization

`DomainGCPRelationshipMaterialization` mirrors `DomainAWSRelationshipMaterialization`
for GCP. It gates on the `cloud_resource_uid` canonical-nodes phase published by
`DomainGCPResourceMaterialization` (the shared `gcp_resource_materialization:<scope>`
acceptance unit), so edges never resolve against uncommitted GCP nodes; the gate
miss is a retryable error so the intent re-enters the durable queue. It loads the
scope generation's `gcp_cloud_resource` and `gcp_cloud_relationship` facts,
builds an in-memory join index keyed by the globally-unique CAI
`full_resource_name`, and resolves both relationship endpoints by exact name —
no per-edge graph round trip, no fuzzy matching.

It honors the provider `support_state` contract: only `supported` relationships
materialize an edge; `partial` relationships treat the target as unresolved (the
collector marks the target opaque/cross-project); `unsupported` relationships are
provenance only. A relationship whose provider type is not a safe Cypher token is
skipped and counted, never failing the batch. Edges are written through the
`GCP_<TYPE>`-prefixed `GCPCloudResourceEdgeWriter` with evidence source
`reducer/gcp-relationships`, distinct from the AWS edge family, and the
prior-generation retract is scoped to that evidence source.

No-Regression Evidence: `go test ./internal/reducer -run
'GCPRelationship|ExtractGCPRelationship' -count=1` and
`go test ./internal/storage/cypher -run 'GCPCloudResourceEdgeWriter' -count=1`
(handler gate/retract/write/error paths, support-state filtering, dedupe,
self-loop, invalid-type skip, and the Cypher writer's MATCH-MATCH-MERGE/retract
shape). Bench `BenchmarkExtractGCPRelationshipEdgeRows` = 30.7 ms/op for 5,000
edges over 10,000 resources vs the AWS `BenchmarkExtractAWSRelationshipEdgeRows`
at 25.9 ms/op on the same host — the same bounded O(R+E) join shape; the GCP edge
writer mirrors the proven AWS `MATCH (source)…MATCH (target)…MERGE` template, so
the only new hot-path Cypher is the `GCP_`-prefixed sibling.

Observability Evidence: the handler emits the `eshu_dp_gcp_relationship_edges_total`
counter dimensioned by bounded `relationship_type` and `join_mode`
(`full_resource_name` / `unresolved` / `partial` / `unsupported` /
`invalid_type` / `empty_type` / `unknown_state`) so an operator can alert on the
GCP edge resolution-failure rate, and wraps the run in the
`reducer.gcp_relationship_materialization` span. The `gcp relationship
materialization completed` structured log carries `scope_id`, `generation_id`,
`domain`, `gcp_resource_fact_count`, `relationship_fact_count`, `edge_count`,
`resolved_count`, `skipped_count`, the same `by_mode` tally,
`unresolved_target_by_type`, `unresolved_source_by_type`, and per-stage
durations, so an operator can answer at 3 AM which GCP relationship target types
are losing edges and why.

### Claim-Time Readiness Enrollment (Ifá P6)

Before this enrollment, `gcp_relationship_materialization` was absent from
BOTH reducer dependency-ordering defenses. The two defenses cover different
sets of sibling domains today, so they are described separately:

- The claim-time readiness CTE
  (`reducerClaimReadinessRequirementsSQL` in
  `go/internal/storage/postgres/reducer_queue_readiness_sql.go`) holds a
  domain unclaimable until its upstream `canonical_nodes_committed` phase
  publishes. The sibling cloud-relationship domains
  (`aws_relationship_materialization`, `azure_relationship_materialization`,
  `workload_cloud_relationship_materialization`) and
  `kubernetes_correlation_materialization` already carry a row here; GCP did
  not, so GCP relationship intents could claim and run before
  `DomainGCPResourceMaterialization` committed the scope's CloudResource
  nodes.
- The non-counting retry-class exemption
  (`nonCountingReducerRetryFailureClasses`, same file) keeps an in-handler
  readiness-gate miss from eroding the reducer's attempt budget. Before this
  change only `secrets_iam` (endpoint) and
  `kubernetes_correlation_materialization` were exempt. The AWS, Azure, and
  workload-cloud relationship siblings have an in-handler `ReadinessLookup`
  miss whose class is NOT yet exempt — a narrower form of the same gap, since
  their claim-time CTE row already blocks the common case (readiness is
  monotonic, so a claimed intent's handler normally sees the same committed
  phase); closing that defense-in-depth gap for them is tracked in #5046. For
  GCP the gap was wide open: with no CTE row the claim gate never blocked, so
  the handler's readiness miss fired on every attempt, and without the
  exemption each `gcp_relationship_nodes_not_ready` retry consumed one of
  `ESHU_REDUCER_MAX_ATTEMPTS` (default 3) — a transient upstream graph-write
  delay on the GCP resource-node dependency then permanently dead-lettered the
  dependent GCP relationship-edge intent instead of waiting for the dependency
  to complete.

The fix adds the missing `('gcp_relationship_materialization',
'cloud_resource_uid', 'canonical_nodes_committed', 'payload_entity_key', '')`
row (identical shape to the AWS/Azure/workload_cloud rows, since the GCP
relationship intent's `EntityKey` already carries the GCP resource domain's
acceptance unit — see [GCP Relationship Edge
Materialization](#gcp-relationship-edge-materialization) above) and exports
`GCPRelationshipNodesNotReadyFailureClass` (mirroring
`KubernetesCorrelationNodesNotReadyFailureClass` and
`SecretsIAMEndpointNotReadyFailureClass`) into
`nonCountingReducerRetryFailureClasses`. This is dependency-ordering, not
serialization: every reducer domain still claims concurrently; a dependent
intent only waits on the specific upstream phase its own edges require to
resolve correctly (accuracy: edges must not resolve against absent nodes).

No-Regression Evidence: the added CTE row is a query-plan no-op for every
other domain. `reducerClaimReadinessGateSQL`'s `NOT EXISTS` subquery
correlates on `readiness_req.domain = work.domain`
(`go/internal/storage/postgres/reducer_queue_readiness_sql.go`), so a new row
for a domain a claim query is not evaluating never participates in that
row's branch. Proven live: `EXPLAIN (ANALYZE, BUFFERS)` of the real
`claimReducerWorkQuery` text (extracted verbatim from the built package)
against Postgres 18 seeded with a representative 60,000-row reducer backlog
(2,000 scopes x 15 domains x 2 statuses, no `graph_projection_phase_state`
rows, i.e. the worst case where every readiness-gated domain must probe the
CTE) filtered to `domain = 'aws_relationship_materialization'` produced
byte-identical plan shape (same join order, same
`fact_work_items_stage_domain_status_idx` bitmap index scan, same
`shared hit=2426/2432` buffer counts) before and after the row addition; the
only delta was the constant-VALUES `CTE Scan on
reducer_claim_readiness_requirements` row/cost estimate moving from
`rows=21 cost=0.42` to `rows=22 cost=0.44` — negligible against the query's
~3024 total cost unit and not a measurable regression against the 896-repo
performance contract. Behavior proof (failing-then-green):
`go test ./internal/storage/postgres -run
'TestReducerQueueClaimWaitsForGCPRelationshipReadinessBehavior|TestReducerQueueFailDefersGCPRelationshipReadinessPastAttemptBudget|TestReducerQueueClaimDoesNotCountGCPRelationshipReadinessDefers|TestClaimBatchDoesNotCountGCPRelationshipReadinessDefers'
-count=1` fails red without the CTE row and the
`nonCountingReducerRetryFailureClasses` enrollment (verified by temporarily
reverting each), and passes green with both. `go test
./internal/storage/postgres ./internal/reducer -race -count=1` stays green.

Observability Evidence: a readiness miss surfaces as the retryable error's
`FailureClass() = "gcp_relationship_nodes_not_ready"`, the same classified-
execution log path as `aws_relationship_nodes_not_ready` and
`kubernetes_correlation_nodes_not_ready`, so an operator can see GCP
relationship intents waiting on GCP resource-node commit. No metric, span,
worker, queue domain, or runtime knob is added or removed; this enrolls an
existing domain into two existing mechanisms.

## Secrets/IAM Trust-Chain Read Model

`DomainSecretsIAMTrustChain` owns the first reducer read model for
`secrets_iam_posture`. Its Postgres loader starts with the trigger
scope/generation, then expands across active generations only through
redaction-safe anchors: `service_account_join_key`, IAM role ARN join values,
web-identity subject fingerprints, Vault policy join keys, and Vault KV path
fingerprints. The reducer classifier admits exact identity chains only when the
path is explicit:

- Kubernetes workload -> ServiceAccount via `k8s_workload_identity_use`
- ServiceAccount -> IAM role through exact IRSA subject fingerprint or EKS Pod
  Identity service-principal trust
- ServiceAccount -> Vault Kubernetes auth role through exact bound service
  account join keys only when the Vault role selectors are not wildcarded
- Vault auth role -> ACL policy -> KV metadata through policy and path
  fingerprints

Wildcard web-identity subjects and wildcard Vault Kubernetes auth-role selectors
remain `privilege_posture_observation` evidence. Missing IAM principals, missing
exact trust, missing workload evidence, stale same-scope generations, hidden
source evidence, unsupported policy layers, and missing Vault policy/KV metadata
become `posture_gap` facts instead of inferred safe or unsafe verdicts. The
writer persists reducer facts only:
`reducer_secrets_iam_identity_trust_chain`,
`reducer_secrets_iam_privilege_posture_observation`,
`reducer_secrets_iam_secret_access_path`, and
`reducer_secrets_iam_posture_gap`.

No-Regression Evidence: `go test ./internal/reducer/... -run 'SecretsIAM|TrustChain'
-count=1` proves exact IRSA, exact EKS Pod Identity, negative name
coincidence, wildcard/broad trust, wildcard Vault selector rejection, stale
generation, unsupported coverage, and handler write behavior. `go test ./internal/storage/postgres -run
'SecretsIAMTrustChain|LoadSecretsIAMTrustChainEvidence' -count=1` proves the
loader stays active-generation scoped, expands through bounded join anchors, and
reports expansion-limit truncation.

Observability Evidence: `eshu_dp_secrets_iam_reducer_trust_chains_total` is
labeled by `result` and `confidence`; `eshu_dp_secrets_iam_posture_observations_total`
is labeled by bounded `risk_type` and `severity`. The handler summary records
seed fact count, loaded fact count, model counts, written fact count, and
whether the loader truncated.

### GCP IAM grant correlation (#2347)

`DomainSecretsIAMTrustChain` consumes the GCP IAM source-fact mirror —
`gcp_iam_principal` (a service-account grantee, join key = the redaction-safe
member fingerprint) and `gcp_iam_permission_policy` (a `(principal, role,
resource)` grant) — emitted by the GCP collector from Cloud Asset Inventory IAM
bindings. The builder indexes both by the shared principal fingerprint and
projects `gcp_service_account_secret_access` (a direct grant on a Secret Manager
secret) and `gcp_service_account_broad_role` (a broad primitive owner/editor
role) privilege-posture observations. A grant with no matching principal fact
never fabricates an identity; a narrow non-secret grant is consumed (indexed and
joined) but yields no observation. The Postgres evidence loader expands across
active generations on the `principal_fingerprint` anchor so a principal fact and
its grants join even when they land in different generations.

The full GCP workload→service-account→secret chain (graph-projected identity
hops) depends on the GCP impersonation / Workload-Identity trust layer tracked in
#2369; this slice delivers the principal and permission layers as posture truth.

No-Regression Evidence: `go test ./internal/reducer/... -run 'GCP.*Grant|GCPSecret|GCPBroad|GCPNarrow'
-count=1`, `go test ./internal/collector/secretsiam -run GCP -count=1`,
`go test ./internal/collector/gcpcloud -run GCPSecretsIAM -count=1`, and
`go test ./internal/facts -run SecretsIAM -count=1`. Bench
`BenchmarkSecretsIAMGCPGrantObservations` = 12.7 ms/op for 4,000 grants over
2,000 principals — bounded O(P+G), no new Cypher (the GCP grants surface as
reducer-owned posture facts, not graph writes).

Observability Evidence: GCP grant observations flow through the existing
`eshu_dp_secrets_iam_posture_observations_total` counter, labeled by the bounded
`risk_type` (`gcp_service_account_secret_access` / `gcp_service_account_broad_role`)
and `severity`, so an operator sees GCP standing-access posture alongside the AWS
wildcard-trust posture without a new metric.

## Multi-Cloud Runtime Drift (issues #1997, #1998, #5759)

Split into [`multi-cloud-runtime-drift.md`](multi-cloud-runtime-drift.md)
(issue #5759 follow-up) to keep this file under the repository's 500-line
cap. That file covers `DomainMultiCloudRuntimeDrift` /
`MultiCloudRuntimeDriftHandler`, the GCP/Azure-vs-AWS provider partitioning
(`excludeAWSOwnedRows`), the read-side AWS aggregation
(`ListActiveFindingsAcrossProviders`), and the full No-Regression /
Observability test evidence.

## S3 External Principal Grant Projection (issue #1231)

`DomainS3ExternalPrincipalGrantMaterialization` loads the same scope
generation's `aws_resource` and `s3_external_principal_grant` facts, resolves
only grants whose source S3 bucket already exists in the CloudResource node
generation, and writes `GRANTS_ACCESS_TO` edges to bounded `ExternalPrincipal`
nodes. Principal identity is stable on `(principal_kind, principal_value)`;
optional account, partition, and service metadata enrich that node only when a
row carries a non-empty value. Unsupported principals, missing identities, and
unscanned source buckets are counted and skipped rather than fabricated.

No-Regression Evidence: `go test ./internal/projector -run
'S3ExternalPrincipalGrant' -count=1`, `go test ./internal/reducer -run
'S3ExternalPrincipalGrant' -count=1`, `go test ./internal/storage/cypher -run
'S3ExternalPrincipalGrant' -count=1`, `go test ./internal/graph -run
'ExternalPrincipal' -count=1`, and `go test ./internal/storage/postgres -run
'S3ExternalPrincipalGrant' -count=1` prove intent emission, exact extraction,
bounded skips, raw-policy redaction, idempotent static-token writes, schema DDL,
and the shared CloudResource readiness gate.

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
'BenchmarkS3ExternalPrincipalGrantWriter|BenchmarkS3LogsToEdgeWriter|BenchmarkCloudResourceEdgeWriter|BenchmarkCloudResourceNodeWriter'
-benchmem -benchtime=100x` writes 5,000 node+edge rows at batch size 500 in
`3.28 ms/op` (`6.49 MB/op`, `35,072 allocs/op`) on darwin/arm64 Apple M4 Pro.
The writer is expectedly heavier than the S3 edge-only writer (`1.39 ms/op`)
because it MERGEs both an `ExternalPrincipal` node and a `GRANTS_ACCESS_TO`
edge, but it remains bounded by one batched statement per 500 rows with no
per-edge graph round trip.

Observability Evidence: `reducer.s3_external_principal_grant_materialization`
wraps fact load, readiness, extraction, retract, and graph write. The
completion log carries resource/grant fact counts, edge count, resolved-outcome
and skipped-reason tallies, first-generation retract decision, and stage
durations; the Cypher writer adds `phase=s3_external_principal_grant` and
`label=ExternalPrincipal` metadata for the existing graph query duration and
batch-size metrics.

## RDS Posture Projection (issue #1233)

`DomainRDSPostureMaterialization` is the RDS posture graph slice. It loads the
same scope generation's `aws_resource` and `rds_instance_posture` facts, resolves
only RDS DB instance and Aurora cluster posture facts whose source resource
already exists in the CloudResource node generation, and stamps properties such
as `rds_public_exposure_state`, `rds_storage_encrypted`,
`rds_backup_retention_period`, `rds_deletion_protection`, IAM DB auth,
Performance Insights, CA certificate, parameter/option group names, and curated
security parameters onto the existing node. A `publicly_accessible=true` fact is
only `candidate_public_endpoint`; internet reachability still requires a later
security-group/path derivation.

No-Regression Evidence: `go test ./internal/reducer -run 'RDSPosture' -count=1`
proves source-resource gating, deterministic dedupe, first-generation retract
skip, retryable readiness misses, additive registry wiring, and handler write
counts.

Observability Evidence: `reducer.rds_posture_materialization` wraps fact load,
readiness, extraction, retract, and graph write. The completion log carries
resource/posture counts, node-update count, skip tally, and stage durations; the
Cypher writer adds `phase=rds_posture` and `label=CloudResource:RDSPosture`
metadata for the existing graph query duration and batch-size metrics.

## EC2 Instance Identity Projection (issue #5448)

`DomainEC2InstanceIdentityMaterialization` is the EC2 instance identity
augmentation slice. It gates on the EC2 instance-node phase
(`ec2_instance_node_materialization:<scope>`, published by
`DomainEC2InstanceNodeMaterialization` from `ec2_instance_posture` facts) —
NOT the generic `aws_resource_materialization:<scope>` phase `DomainRDSPostureMaterialization`
reuses, because the EC2 instance CloudResource node this domain augments is
owned by the posture path, not the generic `aws_resource` node path (which
excludes `aws_ec2_instance` explicitly). It loads the same scope generation's
`aws_resource` facts, filters to `resource_type=aws_ec2_instance`, and stamps
only `ami_id` plus the `ec2_identity_*` provenance properties onto the
already-materialized node.

Dual-writer safety: this domain and `DomainEC2InstanceNodeMaterialization`
target the SAME `cloud_resource_uid` for a given instance from two SEPARATE
scope-generation intents. Their Cypher writers' SET/REMOVE clauses are proven
to share zero property names
(`TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter`,
`go/internal/storage/cypher`), so the write order between the two domains is
commutative and neither domain's retract can delete the other's contribution.
The identity writer is also wrapped in the `graphowner.LockOnlyGate` (like
the RDS/EC2/S3 posture and exposure writers) so its per-uid critical section
serializes against the `#5007` owner-ledger gate's writes to the same uid,
avoiding NornicDB OCC abort-retry churn on concurrent same-uid writes.

The instance->AMI relationship now resolves (issue #5717): the EC2 collector
emits one `aws_resource` fact for the AMI itself (`resource_type=aws_ec2_ami`,
deduplicated per scan across every instance sharing that AMI id — see
`go/internal/collector/awscloud/services/ec2/ami_identity.go`). This is
Pattern A, not a new node class: the AMI materializes under the EXISTING
`CloudResource` label through the SAME generic `aws_resource_materialization`
domain every other resource_type uses, so the generic
`aws_relationship_materialization` domain's target join (`buildCloudResourceJoinIndex`
in `aws_relationship_join.go`) resolves the `aws_ec2_ami` target by bare id
like any other bare-id-keyed resource — no reducer code change was needed for
the join itself. The AMI fact carries only identity (Name is the bare resource
id; no rich state/owner/
creation-date): that AMI metadata requires a `DescribeImages` call the
collector deliberately does not make, a separate, costed enrichment
follow-up.

No-Regression Evidence: `go test ./internal/reducer -run 'EC2InstanceIdentity' -count=1`
proves the augment-only never-create contract, single-key readiness gate,
first-generation retract skip, additive registry wiring, and the cross-domain
disjoint-write proof against a shared instance uid.

Observability Evidence: `reducer.ec2_instance_identity_materialization` wraps
fact load, readiness, extraction, retract, and graph write. The completion
log carries resource fact count, node-update count, first-generation retract
decision, and stage durations; the Cypher writer adds
`phase=ec2_instance_identity` and `label=CloudResource:EC2InstanceIdentity`
metadata for the existing graph query duration and batch-size metrics.

## EC2 Internet Exposure Projection (issue #1301)

`DomainEC2InternetExposureMaterialization` is the conservative EC2 exposure
graph slice. It loads the same scope generation's `ec2_instance_posture`,
`aws_relationship`, and `aws_security_group_rule` facts, derives one
exposed/not_exposed/unknown decision per EC2 instance, and stamps only
reducer-owned `ec2_internet_exposure_*` properties onto existing EC2
`CloudResource` nodes. The decision is exposed only when the instance has a
public IP and an attached security group with internet-reachable ingress.
Missing public-IP, ENI, security-group, or rule evidence stays `unknown` rather
than becoming a safe false; raw public IP addresses are never written.

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
BenchmarkEC2InternetExposureNodeWriter -benchmem -count=3` writes 5,000
MATCH-only node-property rows at batch size 500 on darwin/arm64 Apple M4 Pro in
`1.35 ms/op`, `1.33 ms/op`, and `1.33 ms/op` with about `1.97 MB/op` and
`25,068 allocs/op`. The writer uses batched `UNWIND` + `MATCH
(resource:CloudResource {uid})` + `SET`, so it adds no MERGE, CREATE, per-row
graph round trip, or node fabrication path.

No-Regression Evidence: `go test ./internal/reducer -run 'EC2InternetExposure'
-count=1` proves positive, negative, unknown, missing-identity, readiness, stale
retract, and default registry wiring behavior. `go test ./internal/projector
-run EC2InternetExposure -count=1` proves projector enqueue routing and the
`ec2_instance_node_materialization:<scope>` readiness entity key. `go test
./internal/storage/postgres -run EC2InternetExposure -count=1` proves durable
queue gating and `/admin/status` readiness blockage reporting.

Observability Evidence: `reducer.ec2_internet_exposure_materialization` wraps
fact load, readiness, extraction, retract, and graph write. The new
`eshu_dp_ec2_internet_exposure_decisions_total{outcome,reason}` and
`eshu_dp_ec2_internet_exposure_skipped_total{skip_reason}` counters, completion
log tallies, stage-duration fields, Cypher statement metadata
(`phase=ec2_internet_exposure`, `label=CloudResource:EC2InternetExposure`), and
durable readiness blockage key let an operator distinguish truly exposed
instances from missing topology or rule evidence.

## EC2 Block-Device KMS Posture Projection (issue #1304)

`DomainEC2BlockDeviceKMSPostureMaterialization` loads the same generation's
`ec2_instance_posture`, `aws_resource`, and `aws_relationship` facts, then joins
EC2 block-device volume ids to scanned EBS volume metadata and scanned KMS key
metadata. The result is a bounded EC2 node-property decision:
`encrypted`, `not_encrypted`, `mixed`, or `unknown`. `encrypted` requires every
attached block-device volume to resolve to an encrypted `aws_ec2_volume` with an
`ec2_volume_uses_kms_key` relationship and a scanned `aws_kms_key` whose
`key_manager` is `CUSTOMER`; missing volume facts, missing KMS key facts,
AWS-managed/default keys, detached volumes, tombstones, or ambiguous attachment
evidence stay `unknown`.

The domain gates on both `ec2_instance_node_materialization:<scope>` and
`aws_resource_materialization:<scope>` readiness, writes only bounded scalar/list
properties through `EC2BlockDeviceKMSPostureNodeWriter`, and never creates EC2,
EBS, or KMS nodes. No raw block-device maps, volume contents, snapshots, key
policy bodies, or live AWS calls are part of this reducer. Durable benchmark and
operator-evidence details live in
`docs/public/reference/local-performance-envelope.md`.

## PagerDuty IncidentRoutingEvidence graph projection (issue #1168)

`DomainIncidentRoutingMaterialization` is the conservative graph slice for
PagerDuty incident routing. It loads incident-scoped `incident.record` anchors,
Terraform-source `PagerDutyDeclaration` content rows, same-generation applied
PagerDuty service facts, optional live PagerDuty service facts, and coverage
warnings. The extractor writes graph rows only for:

- declared, applied, and live routing slots that all classify as `exact`; or
- exact live PagerDuty service evidence when declared and applied IaC evidence
  are both missing.

All drifted, stale, permission-hidden, ambiguous, unresolved, rejected, derived,
and missing outcomes stay provenance-only and are counted. Rows use deterministic
`IncidentRoutingEvidence` UIDs and the Cypher writer emits only
`HAS_INTENDED_ROUTING`, `HAS_APPLIED_ROUTING`, and `HAS_LIVE_ROUTING`
relationships between evidence nodes. This domain does not create service,
runtime, image, commit, pull-request, Jira, blast-radius, service-health, or
root-cause edges.

No-Regression Evidence: `go test ./internal/reducer/... -run
'IncidentRouting|DefaultDomainDefinitions.*IncidentRouting' -count=1` proves
exact convergence, live-only no-IaC evidence, unsafe outcome suppression, and
default-domain registration.

Observability Evidence: `eshu_dp_incident_routing_evidence_total` is labeled by
`domain`, bounded `outcome`, `source` (`declared`, `applied`, `observed`, or
`provenance`), and slot `kind`. The handler completion log carries load,
extract, retract, write, and total durations plus materialized/skipped tallies.
