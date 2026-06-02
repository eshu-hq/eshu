# Design: EC2 instance canonical CloudResource node + readiness phase — PR-A of the #1146 blast-radius arc

**Status:** NEEDS PRINCIPAL REVIEW — this PR makes EC2 instances first-class
canonical graph nodes for the first time, reversing the EC2 scanner's deliberate
"no instance inventory" choice (owner-approved). `risk:schema`, reducer
graph-write. No auto-merge. The node-identity / keyspace / property-set decisions
below are the principal-review focus: a mis-keyed or over-broad instance node is
an accuracy/secrecy failure.

**Related:** #1146 (aws/deep: EC2 instance blast-radius). The shipped #805
CloudResource node + AWS relationship edge materialization
(`docs/internal/aws-relationship-edge-materialization-design.md`) is the keyspace
this slice rides; the shipped #388 PR2 KubernetesWorkload node
(`docs/internal/design/388-kubernetes-workload-node.md`) is the
posture-fact-driven node-materialization playbook this slice mirrors; the merged
#1144 PR2 S3 `LOGS_TO` and #1134 PR2 `CAN_ASSUME` slices are the
node-then-gated-edge pattern PR-B will follow.

## Why

The EC2 scanner (`go/internal/collector/awscloud/services/ec2/scanner.go:13-17`,
`constants_ec2.go:20-23`) **deliberately does not emit an `aws_resource`
inventory fact for instances** — it emits VPC / Subnet / SecurityGroup /
SecurityGroupRule / NetworkInterface `aws_resource` nodes and one metadata-only
`ec2_instance_posture` fact per instance, but no instance node. EC2 instances are
high-cardinality and ephemeral, which is the likely reason the scanner skipped
them.

The repo owner has approved making EC2 instances first-class `:CloudResource`
nodes, because the blast-radius chain
**EC2 → instance-profile → role → escalation** cannot be drawn without an
instance node to anchor its first edge. This PR-A builds **only that node**. It
draws **no edge**. The two immediate follow-ups are recorded in §Deferred and
built in later PRs.

## Scope of this slice

1. **EC2 instance canonical node.** A reducer domain
   (`DomainEC2InstanceNodeMaterialization`) that materializes
   `ec2_instance_posture` facts into canonical `:CloudResource` graph nodes on the
   **existing** `cloud_resource_uid` keyspace, plus the backend-neutral
   `EC2InstanceNodeWriter` (`go/internal/storage/cypher`). No new node label, no
   new keyspace, **no schema DDL** — the `:CloudResource` label, the
   `cloud_resource_uid_unique` constraint, and the lookup indexes already exist
   from #805 (`go/internal/graph/schema.go:131,234-236`,
   `schema_cloud_resource_test.go:11`).
2. **Readiness phase.** The handler publishes
   `GraphProjectionPhaseCanonicalNodesCommitted` on
   `GraphProjectionKeyspaceCloudResourceUID` after the node write succeeds (or is
   a legitimate empty-generation no-op), through the existing durable
   `graph_projection_phase_state` publisher — under a **distinct** entity key (see
   §Readiness wiring).
3. **Projector trigger.** A projector intent
   (`buildEC2InstanceNodeMaterializationReducerIntent`) enqueues the node domain
   when any `ec2_instance_posture` fact is present in a scope generation.

Deferred (stated so reviewers do not expect them here):

- **PR-B — `USES_PROFILE` edge** `(:CloudResource {ec2})-[:USES_PROFILE]->
  (:CloudResource {instance-profile})`. The posture fact already carries
  `instance_profile_arn`. PR-B resolves it to the instance-profile CloudResource
  node and writes the gated edge, mirroring #1144 PR2 `LOGS_TO`. It gates on
  `cloud_resource_uid` for **two** entity keys: the instance-profile node
  (published by `aws_resource_materialization`) and the instance node (published
  by this slice's `ec2_instance_node_materialization`) — exactly like the #1135
  reachability edge gates on three phases. PR-B adds the gate clause; this slice
  adds none (see §Readiness wiring).
- **profile → role edge** `(:CloudResource {instance-profile})-[:HAS_ROLE]->
  (:CloudResource {role})` — a later follow-up that completes the
  EC2 → profile → role → `CAN_ESCALATE_TO` (#1134 PR3) blast-radius chain.

## Node-identity / keyspace / label / property decisions (principal-review focus)

| Decision | Value | Why (not invented) |
| --- | --- | --- |
| **Node identity / `uid`** | `cloudResourceUID(account_id, region, "aws_ec2_instance", instance_id)` | The exact `cloudResourceUID(accountID, region, resourceType, resourceID)` helper (`go/internal/reducer/aws_resource_materialization.go:247`). `resource_type` token is `aws_ec2_instance` (`ResourceTypeEC2Instance`, `constants_ec2.go:23`); `resource_id` is the bare instance id (`i-…`), with ARN fallback when the instance id is blank, matching the posture envelope's own `identity` derivation (`ec2_posture_envelope.go:29-32`). **This is the scheme `go/internal/reducer/observability_coverage_correlation_test.go:29` already assumes** for an EC2 instance uid, so an alarm whose `InstanceId` dimension resolves to this instance now resolves to a node that actually exists. Confirmed, not guessed. |
| **Keyspace** | `cloud_resource_uid` (existing) | An EC2 instance **is** a CloudResource. The USES_PROFILE edge (PR-B) joins two CloudResource nodes (instance, instance-profile), both on `cloud_resource_uid`. Reusing the keyspace lets PR-B gate exactly like #1134 `CAN_ASSUME` / #1144 `LOGS_TO`. |
| **Node label** | `:CloudResource` (existing) | No new label. The instance node carries `resource_type = aws_ec2_instance`, the same discriminator every other AWS resource node uses. The `cloud_resource_uid_unique` constraint already covers it. |
| **Readiness phase** | reuse `GraphProjectionPhaseCanonicalNodesCommitted` on `cloud_resource_uid`, **distinct entity key** | See §Readiness wiring. |
| **Domain** | `DomainEC2InstanceNodeMaterialization`, additive | Registered only when `EC2InstanceNodeWriter` + `FactLoader` are wired, so an intent is never silently dropped by a handler that cannot write (the `appendAdditiveDomainDefinitions` honest-additive pattern). |

### Node property set (metadata-only, bounded, stable)

The node carries the stable identity properties every CloudResource node carries,
plus the derived posture booleans/scalars the `ec2_instance_posture` fact already
emits — and **nothing else**:

- **Identity (shared CloudResource shape):** `uid`, `id` (= uid), `arn`,
  `resource_id` (instance id), `resource_type` (`aws_ec2_instance`), `name`
  (instance id; the posture fact carries no Name-tag, so the id is the stable
  name and no tag value is read), `state`, `account_id`, `region`,
  `service_kind`, `correlation_anchors`, and the `source_*` / `collector_kind` /
  `evidence_source` provenance fields.
- **Derived posture (safe scalars/booleans only):** `imds_v2_required`,
  `imds_http_endpoint`, `imds_http_put_hop_limit`, `user_data_present` (presence
  boolean, NEVER content), `detailed_monitoring_enabled`, `ebs_optimized`,
  `public_ip_associated`, `instance_profile_arn`, `tenancy`,
  `nitro_enclave_enabled`.

**Deliberately excluded** (no fabrication, no secret surface):

- **User-data content, console output, env vars, command-line args, raw config /
  policy blobs** — never on the fact, never on the node. Only `user_data_present`
  (a boolean) is carried.
- **Tag values** — the posture fact carries no tags; the node reads none, so no
  secret can leak through a tag value.
- **`public_ip_address`** — the raw public IP is a routable network identifier;
  it is omitted from the node to keep the node a bounded posture/identity surface.
  `public_ip_associated` (the boolean) is sufficient for blast-radius/internet-
  exposure reasoning, which is the #1135-style derived-flag follow-up's job.
- **`block_devices`** — a list of per-volume maps, not a scalar or homogeneous
  scalar list, so it is not a valid graph property value; the block-device → KMS
  join is a reducer-owned follow-up (#1146) that reads the fact directly, not the
  node.
- **`instance_type`, `availability_zone`, `vpc_id`, `subnet_id`** — the
  `ec2_instance_posture` fact does **not** carry these fields (verified against
  `go/internal/collector/awscloud/ec2_posture_envelope.go` and the scanner
  `Instance` type; `InstanceType`/`SubnetID`/`VPCID` exist on the scanner-owned
  struct but are not projected onto the posture payload). The node therefore does
  **not** assert them — materializing absent data would be fabrication. If a later
  slice promotes those fields onto the posture fact, they become node properties
  then, not now.

`instance_profile_arn` is carried as a **node property only** in this slice; PR-B
turns it into the `USES_PROFILE` edge. Carrying it as a property now lets an
operator see "which profile does this instance use" before the edge lands, and
gives PR-B a node-local anchor.

## Idempotency, concurrency, ordering

- The node write is `MERGE (r:CloudResource {uid: row.uid})` on the uid only;
  mutable properties `SET` separately. Duplicate posture facts (retries,
  overlapping scans) and reducer reprojection converge on one node via the
  `cloud_resource_uid_unique` constraint — idempotent under concurrent execution.
  The conflict key is the per-instance uid; there is no contended write, so this
  is **not** a "serialization is not a fix" case — no worker-count reduction,
  single-threaded drain, or batch size 1 is introduced.
- An EC2 instance arrives **only** via the `ec2_instance_posture` fact (the
  scanner emits no `aws_resource` fact for instances), so the EC2 node writer and
  the #805 `aws_resource` node writer never MERGE the same uid for the same
  instance — no cross-writer race on a shared uid. They share the label and
  constraint, which is the intended idempotent convergence point, not a conflict.
- `ExtractEC2InstanceNodeRows` deduplicates by uid and sorts by uid, so the
  batched write is byte-stable across retries and reprojections.
- Tombstoned posture facts (a terminated instance no longer running) materialize
  no node — reading one would assert a node for an instance that no longer exists.
- The readiness phase is published **only after** the node write succeeds (or is
  a legitimate empty-generation no-op). Publishing before a successful write would
  let PR-B resolve edges against nodes that never committed; not publishing on an
  empty generation would block PR-B forever. Both invariants are covered by tests.

## Readiness wiring (why a distinct entity key, why no gate clause here)

The EC2 node domain publishes `cloud_resource_uid` / `canonical_nodes_committed`
under entity key **`ec2_instance_node_materialization:<scope>`**, NOT the
`aws_resource_materialization:<scope>` key the #805 node domain uses.

This is a deliberate correctness decision. If both the `aws_resource` node domain
and the EC2 node domain published the **same** `(cloud_resource_uid,
canonical_nodes_committed, aws_resource_materialization:<scope>)` phase row, an
edge gating on that single row could open as soon as **either** domain committed
— before the other's nodes landed — letting an edge resolve against
not-yet-committed instance nodes. Distinct entity keys make the two node phases
independent: PR-B's `USES_PROFILE` edge gates on `cloud_resource_uid` for **both**
keys (the instance-profile node under `aws_resource_materialization:<scope>`, the
instance node under `ec2_instance_node_materialization:<scope>`), exactly like the
#1135 reachability edge gates on three phases. The `graph_projection_phase_state`
upsert is keyed on the full composite
`(scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)`
(`go/internal/storage/postgres/graph_projection_phase_state.go:43`), so two
distinct entity keys produce two distinct rows — no collision, no overwrite.

**This slice adds NO gate clause** to the durable Postgres claim/blockage gate.
PR-A is a node **publisher**, not an edge consumer; the gate's `EXISTS` guards
fence edge domains (`aws_relationship_materialization`, etc.) on the node phase.
The `ec2_uses_profile_materialization` edge domain does not exist yet (PR-B), so
adding a gate clause now would reference a non-existent domain — the exact
non-existent-domain trap the #388 KubernetesWorkload node design called out. PR-B
adds the `ec2_uses_profile_materialization` clause to
`reducer_queue_claim_query.go`, `reducer_queue_batch.go`, and `status_blockage.go`
keyed on `cloud_resource_uid` for both entity keys.

The durable-gate proof in this slice
(`reducer_queue_ec2_instance_node_readiness_test.go`) therefore proves the
existing `cloud_resource_uid` gate is satisfied by the EC2 node domain's
published phase row: an edge gated on `cloud_resource_uid` under the EC2 node
entity key stays unclaimable until the EC2 node phase row exists, then becomes
claimable. This proves the new node source feeds the durable gate without
inventing a non-existent edge domain.

## Edge cases (covered by tests)

- **Empty** — no posture facts → zero rows, no write, phase still published.
- **Tombstone** — a terminated/tombstoned instance materializes no node.
- **Missing optional fields** — a posture fact without IMDS/profile fields still
  materializes a node with the fields present; absent fields are not fabricated.
- **Partial / missing identity** — a posture fact without both instance id and
  arn cannot form a uid and is dropped, never fabricated.
- **Duplicate** — duplicate facts converge on one node.
- **Deterministic uid** — uid is independent of input ordering; rows sort by uid.

## Performance impact declaration (eshu-diagnostic-rigor)

- **Affected stage:** new reducer node materialization
  (`DomainEC2InstanceNodeMaterialization`) + the `cloud_resource_uid` readiness
  publish. One intent per scope generation that has `ec2_instance_posture` facts.
- **Expected cardinality:** one candidate node per posture fact = one per running
  instance per account/region. EC2 instances can be **high-cardinality and
  ephemeral** (the likely reason the scanner skipped instance inventory). The
  extractor dedupes by uid and sorts; the write is batched at the shared
  `DefaultBatchSize` (500) so a region with N instances costs `ceil(N/500)`
  UNWIND statements — bounded, no per-instance graph round trip, no N+1.
- **Baseline / known-normal band:** the shipped #805
  `BenchmarkCloudResourceNodeWriter` and #388
  `BenchmarkKubernetesWorkloadNodeWriter` on the identical 5000-row /
  batch-500 / no-op-group-executor shape. The EC2 writer reuses the exact
  UNWIND-batched MERGE-on-uid shape, so its per-op time must stay within noise of
  those baselines.
- **Proof ladder:** focused reducer extractor + handler tests → cypher writer
  static-shape tests → a writer benchmark vs the CloudResource baseline →
  postgres readiness-gate query test. No new query shape vs the shipped
  CloudResource node writer, so a no-regression argument on the same write shape
  stands in for a fresh full-corpus bench in this slice.
- **Stop threshold:** the bench measures only in-memory row-clone + statement
  construction (no-op executor); the relevant production threshold is the graph
  round-trip per-row cost, which is unchanged (same MERGE-on-uid shape, no new
  index/constraint). If a corpus run shows the EC2 instance node *graph write*
  per-row time exceeding the #805/#388 node-write per-row time by more than ~10%
  or ~60s, profile before merge.

Benchmark Evidence: `go test ./internal/storage/cypher -run XXX_none -bench
'BenchmarkEC2InstanceNodeWriter|BenchmarkCloudResourceNodeWriter|BenchmarkKubernetesWorkloadNodeWriter'
-benchmem -benchtime=2s -count=3` on darwin/arm64 (Apple M-series), no-op group
executor, 5000 node rows at the default 500/UNWIND:

```
BenchmarkCloudResourceNodeWriter-12         ~2.88 ms/op   6327686 B/op   25068 allocs/op
BenchmarkKubernetesWorkloadNodeWriter-12    ~2.87 ms/op   6327800 B/op   25068 allocs/op
BenchmarkEC2InstanceNodeWriter-12           ~3.79 ms/op   6327990 B/op   25068 allocs/op
```

The EC2 writer's per-op time is ~32% above the 16-property CloudResource /
KubernetesWorkload baselines, with an **identical allocation count**
(`25068 allocs/op`) and effectively identical bytes. The delta is **not** a new
per-row cost class or a graph-path regression: it is the in-memory clone of a
wider property map — the EC2 node carries 26 row keys (16 shared identity/provenance
+ 10 derived posture) vs 16 on the baseline — copied once per row in the writer's
`annotated` step to keep caller rows immutable. The cost scales linearly with the
property count, is fully in-memory, and is dwarfed in production by the graph
round-trip the no-op executor elides. This is the bounded, expected cost of a
richer node, captured honestly rather than hidden.

No-Regression Evidence: the EC2 node write is byte-for-byte the same
UNWIND-batched `MERGE (:CloudResource {uid})` + `SET` shape as the shipped #805
CloudResource node writer; the only difference is ten additional `SET` scalar
properties (the posture booleans/scalars), which add **no new per-row graph cost
class**, no new query shape, and no new index/constraint (the
`cloud_resource_uid_unique` constraint and the arn/resource_id/type lookup indexes
already exist from #805). The write is bounded by `ceil(N/batchSize)` statements;
write-amplification is one uid index entry per node, identical to every other
CloudResource node. The ~32% in-memory bench delta is the wider-map clone above,
not a graph-write regression.

Observability Evidence: the new `eshu_dp_ec2_instance_nodes_total{domain}` counter
(materialized node count, recorded even at zero so a generation that produced no
nodes is visible), the `eshu_dp_ec2_instance_nodes_skipped_total{reason}` counter
(`missing_identity` / `tombstone` conservative skips), the `ec2 instance node
materialization completed` structured completion log with per-stage durations
(load / extract / graph_write / phase_publish) and node/skip counts, and the
InstrumentedExecutor's `eshu_dp_neo4j_query_duration_seconds` /
`eshu_dp_neo4j_batch_size` on each `phase=ec2_instance` / `label=CloudResource`
statement let an operator see instance-node throughput, the skip class, and a
generation that committed zero nodes, at 3 AM.

## Open items for principal review

1. **Reversing "no instance inventory."** This slice makes EC2 instances
   first-class `:CloudResource` nodes for the first time, reversing the scanner's
   deliberate choice (owner-approved). Confirm the approval still stands and that
   the high-cardinality/ephemeral cost is accepted given the bounded batched
   write.
2. **Property set.** Confirm the metadata-only property set above — specifically
   the exclusion of `public_ip_address`, `block_devices`, and the
   not-on-the-fact `instance_type`/`vpc_id`/`subnet_id`/AZ — is the right bounded
   surface, and that `instance_profile_arn` should ride as a node property in
   PR-A ahead of the PR-B edge.
3. **Distinct entity key.** Confirm the `ec2_instance_node_materialization:<scope>`
   entity key (distinct from `aws_resource_materialization:<scope>`) before PR-B
   locks the two-key `USES_PROFILE` gate. Changing it later is a `risk:schema`
   coordination change.
