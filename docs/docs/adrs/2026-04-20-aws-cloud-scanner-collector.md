# ADR: AWS Cloud Scanner Collector

**Date:** 2026-04-20
**Status:** Accepted with follow-up
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `2026-04-20-terraform-state-collector.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md` — fact-field
  back-propagation source for the reducer/consumer contract this ADR now
  incorporates directly.

---

## Status Review (2026-05-10)

**Current disposition:** Design accepted; deployment model amended.

The original ADR chose a single central `collector-aws-cloud` deployment with
STS `AssumeRole` per claim. That remains the default for teams that can grant a
central Eshu control plane scoped cross-account read roles.

The design now also supports account-local collector deployments. Some
customers will prefer, or require, one collector per AWS account, organization
unit, or regulated boundary. The scanner must therefore share the same
provider-neutral target-scope and credential model as the Terraform-state
collector: central assume-role and local workload identity are both valid
deployment modes, and the coordinator/fact contracts must not depend on which
one was chosen.

## Status Review (2026-05-03)

**Current disposition:** Design accepted; runtime not implemented.

The shared identity and workflow substrate exists, including the AWS collector
kind and reducer phase contract. The actual collector runtime does not exist in
this repo yet: there is no `go/cmd/collector-aws-cloud` or
`go/internal/collector/awscloud` implementation.

**Remaining work:** implement the collector runtime, IAM-first scanner, service
packages, claim loop, AWS telemetry, fact emission, correlation consumers,
tests, and operator docs.

## Context

Eshu already correlates Terraform *configuration* from Git,
and the companion Terraform state collector ADR adds what Terraform *believes*
it built. Neither of those is sufficient to describe what AWS actually runs.

Many organizations, including parts of the platforms the Eshu targets, do not
provision every resource through Terraform. Pulumi, CloudFormation, CDK, SAM,
Serverless Framework, and direct console or CLI actions are all in scope.
Without direct cloud observation, Eshu cannot:

- see resources provisioned outside Terraform
- detect orphaned resources
- anchor canonical identity on real ARNs observed by the cloud API
- confirm hostnames, target groups, and load balancer bindings
- correlate runtime platform placement (ECS service, EKS workload, Lambda
  alias) with source repositories and IaC
- report on tags as they actually exist on cloud resources rather than as
  they were declared

The multi-source correlation DSL ADR already named `aws` as a first-class
collector family. The workflow coordinator ADR already defined the runtime
shape for `collector-aws-cloud` and reserved a place in the runtime contract.
This ADR is the collector-specific design ADR those two ADRs deferred.

### What Is Already Decided

This ADR inherits and does not revisit:

1. Collectors emit typed facts. They do not write canonical graph rows.
2. The reducer owns cross-source correlation through the correlation DSL.
3. The workflow coordinator owns scheduling, claim issuance, fencing, and
   completeness. The AWS scanner claims bounded work; it does not self-
   schedule.
4. `CollectorKind.aws` is part of the shared identity enum.
5. Collector instances are declarative. Each AWS account is one instance.

### What This ADR Decides

1. The scanner framework choice: roll own against AWS SDK for Go v2.
2. Deployment topology: central deployment by default, account-local
   deployment when customer boundaries require it.
3. Claim granularity and its relationship to AWS throttling boundaries.
4. Launch resource coverage and deferred coverage.
5. Rate discipline, concurrency bounds, and retry posture.
6. Credential model: workload identity plus scoped target credentials. No
   static keys.
7. Scope and generation identity for cloud observations.
8. Fact shapes and the tag-raw-emission contract.
9. Phased rollout, including which correlation DSL work depends on the
   scanner's output.

---

## Problem Statement

The platform must decide how AWS scanner runtimes can observe cloud state
across many accounts and many regions:

- safely under throttling limits
- without turning into a cloud security incident through over-privileged
  credentials
- without regressing the coordinator's invariants around fencing and
  completeness
- at a cadence that is useful without being abusive
- with telemetry strong enough to debug and tune from dashboards
- with tag and ARN emission that the correlation DSL can turn into canonical
  joins

The same runtime must scale from "one small account" to "dozens of accounts,
multiple regions each, many resource families" without demanding a separate
deployment per account or per region.

---

## Decision

### Framework: Roll Own Against AWS SDK For Go v2

The scanner should be built in-house against the AWS SDK for Go v2. The
platform should not adopt Cartography, Steampipe, AWS Config as a primary
source, or a third-party cloud CMDB as the scanner implementation.

Rationale:

- **Cartography** writes directly to a graph. It violates the Eshu rule that
  collectors emit facts and the reducer owns canonical writes. Adopting it
  would require gutting its write path or running a second graph. Neither is
  acceptable.
- **Steampipe** is SQL-over-API: an operator query surface rather than a
  fact emitter. It is not designed to be the upstream of a durable fact
  queue with scope and generation identity.
- **AWS Config** gives delta events but requires org-wide Config enablement,
  which the platform cannot assume across all customers. Config is useful
  as an *optional* freshness source later; it is not the baseline.
- **Third-party CMDBs** add licensing, vendor dependency, and opaque
  correctness models. The platform's accuracy-first priority demands we
  own the observation path.

Rolling our own has real costs: schema maintenance per service, throttling
discipline, pagination logic, and cross-service dependency ordering. The
alternative is worse: misaligned ownership, loss of invariants, or both.

### Topology: Central Default, Account-Local Supported

The AWS scanner supports two deployment modes.

The default mode is a central `collector-aws-cloud` deployment. It runs in the
Eshu control-plane account or cluster and assumes target-account read roles per
claim.

Inside that deployment:

- a bounded worker pool drains coordinator claims
- each claim carries `(collector_instance, account, region, service_kind)`
- each worker performs `sts:AssumeRole` against the claim's account role ARN
  at claim-start time, caches the resulting credentials until the claim's
  lease expiry, and discards them on release
- no static access keys are loaded into the pod at any time

The second supported mode is account-local deployment. A customer may run a
collector in each account or organization boundary and let that workload use a
local role. The coordinator still assigns claims and the collector still emits
the same AWS facts. Only credential acquisition changes.

This is explicitly not:

- long-lived cached cross-account credentials (blast radius)
- one fact schema per deployment mode
- a reason to bypass coordinator claims, fencing, or rate limits

Central deployment is operationally simpler. Account-local deployment narrows
blast radius and may fit customer trust boundaries better. Eshu must support
both without forking the scanner architecture.

### Claim Granularity Matches Throttling Boundaries

AWS throttles API calls on a scope of roughly `(account, region, service)`
with some sharing across a control plane. Claim granularity must match:

```
claim_key = (collector_instance_id, account_id, region, service_kind)
```

The coordinator's existing claim-uniqueness and fencing contract then
enforces that at most one worker is actively calling `ecs.describe_*` in
`acct123/us-east-1` at any moment. This alone prevents self-inflicted
throttle wars.

Cross-account scans run fully in parallel. Cross-region within the same
account run in parallel. Cross-service within the same account+region run
concurrently up to a per-account concurrency cap (see below) to avoid
hammering the shared control plane.

### Launch Resource Coverage

Phase 1 covers the services that produce the highest-value correlation anchors
for container and service delivery, matching the DSL ADR's container vertical
slice:

| Service | Resources | Why |
| --- | --- | --- |
| **IAM** | Roles, Policies, InstanceProfiles, trust relationships | Trust chain from code to runtime identity |
| **ECR** | Repositories, Images (tags, digests), lifecycle policies | Image identity, the strongest code→runtime join |
| **ECS** | Clusters, Services, TaskDefinitions (redacted), Tasks | Runtime placement for non-EKS workloads |
| **EKS** | Clusters, Nodegroups, add-ons, OIDC provider | Runtime placement for Kubernetes workloads |
| **ELBv2** | LoadBalancers, Listeners, TargetGroups, Rules | Hostname + routing evidence |
| **Lambda** | Functions, Aliases, EventSourceMappings | Function-as-service runtime identity |
| **Route53** | HostedZones, Records (A, AAAA, CNAME, ALIAS) | Public hostname evidence for ALB/CloudFront targets |
| **EC2** | VPCs, Subnets, SecurityGroups, ENIs (metadata only) | Network topology referenced by ECS/EKS/Lambda |

Phase 2 (explicit non-goal for launch but planned):

- RDS, DynamoDB (data-plane resources)
- S3 (bucket metadata only; never object contents)
- SQS, SNS, EventBridge (messaging surface)
- CloudFront, API Gateway (edge surface)
- Secrets Manager, SSM Parameter Store (metadata only; values never read)
- CloudWatch Logs (group metadata only; log contents never read)

Phase 3+ covers additional services on operator demand.

### Scan Model: Full Scheduled Scans, With AWS Config As Later Freshness Layer

Launch scan behavior:

- full `List*` + `Describe*` sweep per `(account, region, service)` on a
  schedule
- default refresh interval per service family configurable per instance
- conditional refresh uses the scanner's own checkpoints
  (`last_seen_etag`-equivalent for each resource type) to skip re-emission
  when the cloud API did not actually change observed state, but the scanner
  still *queries* the API each cycle; cache is for fact emission, not for
  call avoidance
- EventBridge / AWS Config integration is a **phase 3** optional freshness
  layer. When enabled, it converts cloud change events into coordinator run
  requests targeted at the affected `(account, region, service)` tuple.
  Baseline full scans remain.

This model keeps the scanner simple and correct first, and opens a path to
lower-latency freshness once the baseline is trusted.

### Rate Discipline

Required behavior:

1. **Per-service token bucket** inside each worker, sized per-service and
   configurable per collector instance. Defaults are conservative.
2. **SDK v2 adaptive retry mode** enabled on every client. No retries
   disabled anywhere.
3. **Pagination pacing** for high-cardinality services (EC2, Lambda,
   CloudWatch Logs). A small sleep between pages when a run exceeds a
   configurable page count. Backoff on throttle.
4. **Per-account concurrency cap** across the worker pool: a bounded
   in-flight claim count per `account_id` regardless of service. Default
   `4`. Prevents surge when many services in the same account become
   claimable simultaneously.
5. **Hard API budget per scan** per `(account, region, service)`. When
   exceeded, the scan is marked `budget_exhausted`, emits an `aws_warning`
   fact, and yields the claim. The next coordinator run resumes from the
   last page checkpoint.
6. **Throttle observability.** `aws_throttle_total{service, account,
   region}` is a first-class metric. Operators must be able to see throttle
   events as they happen.

### Credential Model: IRSA + Cross-Account AssumeRole + External ID

The scanner runtime runs under an IRSA-bound service account in the hosting
cluster.

For each collector instance (= AWS account):

- operator configures `role_arn` in the target account
- operator configures `external_id` supplied through the platform's secret
  source, not committed to config
- the target role grants only read permissions required for the declared
  service set
- the trust policy restricts principals to the scanner's IRSA principal and
  requires the external ID

At claim time:

1. worker calls `sts:AssumeRole` with the configured `role_arn` and external
   ID
2. worker receives short-lived credentials (15 minutes default, tuned to
   claim lease)
3. credentials are used only for the duration of this claim
4. on claim completion or expiry, credentials are zeroed from memory

No long-lived keys, no env-var-injected credentials, no role chaining beyond
the cross-account hop.

### Scope And Generation Identity

Scope: `ScopeKind.account` with attributes `(account_id, region)`. No new
scope kind is introduced; the multi-source correlation ADR already committed
to `account`, and adding `cloud_region` now would splinter scope identity.
Region is carried as a scope attribute.

### Required Reducer/Consumer Contract Fields

The accepted multi-source reducer/consumer ADR freezes the AWS scanner fields
that downstream joins and drift queries require. These are first-class fact
fields, not query-time reconstructions.

Add to `aws_resource_fact`:

- `arn` — required for every resource type that has an ARN
- `resource_type` — frozen enum matching the reducer-owned canonical label
- `correlation_anchors` — `[]string` containing deterministic join anchors
  such as ARN, normalized hostname, image digest, or normalized tags

Add to the shared AWS fact envelope:

- `scope_id`
- `collector_kind=aws`
- `generation_id`
- `fence_token`
- `source_confidence`

Normalization contract:

- the scanner emits raw `tags`
- `tags_normalized` is populated later by `reducer/tags/normalizer`
- downstream queries must treat `tags_normalized` as a canonical-node field,
  not a raw collector field

Resources without ARNs must document a deterministic fallback key family in
the collector implementation plan. Examples include ECS task-definition
family+revision or IAM policy version identity. Those fallback keys must also
be surfaced through `correlation_anchors`; they must never remain implicit in
opaque payload blobs.

Generation: monotonic per `(account_id, region, service_kind)`, assigned by
the scanner at claim start from a coordinator-provided monotonic counter.
This gives a stable "which scan produced this fact" anchor without requiring
the cloud API to provide one.

Each resource fact also carries, where available:

- the AWS-reported `last_modified` or equivalent timestamp
- a resource-level digest computed by the scanner over the normalized
  observed attributes, used for unchanged detection on the next scan

### Fact Shapes

The scanner emits a bounded set of fact kinds. Per-service schemas live in
versioned packages; the envelope is shared.

| Fact Kind | Purpose |
| --- | --- |
| `aws_resource` | one per observed resource; carries `arn`, `resource_type`, `resource_id`, `region`, `account_id`, `tags`, `state`, `created_at`, `modified_at`, typed attributes |
| `aws_relationship` | one per observed relationship (ECS service→task def, ALB listener→target group, Lambda alias→function, IAM role→policy, ENI→subnet) |
| `aws_tag_observation` | tag key/value pairs emitted separately for correlation indexing, preserving account + region + resource ARN |
| `aws_dns_record` | Route53 record emitted as its own shape because of its join importance |
| `aws_image_reference` | ECR image digest + tag + repository + pushed_at; separate shape because images are the single most important code→cloud join |
| `aws_warning` | anti-pattern signals (`plaintext_env_var`, `public_s3_metadata_flag`, `budget_exhausted`, `unknown_resource_schema`, `throttle_sustained`, `assumerole_failed`) |

Every fact carries:

- `scope_id`, `generation_id`, `collector_kind = aws`
- `account_id`, `region`, `service_kind`
- `collector_instance_id`

### Raw Tag Emission

Tags are emitted unchanged. Keys preserve original case and exact spelling.
Values preserve original case and spelling.

Tag normalization is **not** the collector's concern. Aliasing (`env`,
`environment`, `Env`, `ENV`), value normalization (`prod` vs `production`),
precedence against TF state tags, exclusion rules (`DoNotIndex`,
`Name:^(test|scratch|tmp)-`), and confidence scoring all live in the
correlation DSL.

The collector also emits:

- `aws_tag_distribution` summary facts per `(account, region, resource_type,
  tag_key)` with distinct-value count and total occurrences. These feed a
  later operator-facing "suggest alias" workflow in the DSL admin surface.

### Sensitive Value Discipline

Some AWS resources leak secrets through attributes:

- Lambda function environment variables
- ECS task definition container environment variables and secrets fields
- RDS master credentials (when returned)

The scanner applies redaction analogous to the state collector:

- env var values replaced with `sha256:<hex>`
- secret references (ARN) preserved as ARN (not a leak)
- attribute keys on a shared sensitive-key list redacted by default
- telemetry never carries plaintext values

Cloud scanner redaction and state collector redaction share the same library
(`go/internal/redact`) so the policy stays unified.

---

## Architecture

### Runtime Shape

- binary: `/usr/local/bin/eshu-collector-aws-cloud`
- package: `go/cmd/collector-aws-cloud/`
- internal: `go/internal/collector/awscloud/`
- Kubernetes: `Deployment`, horizontally scalable under the coordinator
  claim model
- deployment count follows the target-scope credential model: one central
  deployment by default, or one account-local deployment per boundary when
  that is the safer customer choice

### Package Layout

```text
go/
  cmd/
    collector-aws-cloud/
      main.go
  internal/
    collector/
      awscloud/
        service.go              # coordinator claim loop
        worker.go               # per-claim worker
        credentials.go          # STS AssumeRole, caching, zeroing
        throttle.go             # per-service token buckets
        pagination.go           # shared pagination + pacing
        facts.go                # fact envelope builders
        redact.go               # thin wrapper over go/internal/redact
        telemetry.go            # span/metric helpers
        services/
          ec2/
          ecr/
          ecs/
          eks/
          elbv2/
          iam/
          lambda/
          route53/
```

Each service package exposes a small interface:

```go
type ServiceScanner interface {
    Kind() ServiceKind
    Scan(ctx context.Context, claim Claim, emit FactEmitter) (ScanResult, error)
}
```

This keeps per-service code isolated and allows independent testing and
evolution.

### Collector Instance Configuration

```yaml
collectors:
  - id: aws-prod-main
    kind: aws
    mode: scheduled
    enabled: true
    bootstrap: true
    config:
      account_id: "123456789012"
      account_alias: prod-main
      role_arn: arn:aws:iam::123456789012:role/eshu-collector
      external_id: ${secret:aws.prod-main.external_id}

      regions:
        - us-east-1
        - us-west-2

      services:
        - iam
        - ecr
        - ecs
        - eks
        - elbv2
        - lambda
        - route53
        - ec2

      max_concurrent_claims: 4
      per_service_rps:
        ec2: 5
        lambda: 5
        elbv2: 10
        ecs: 10
        ecr: 10
        route53: 5
        iam: 5
      scan_interval_default: 30m
      scan_interval:
        iam: 6h
        route53: 1h
      page_pacing_threshold: 50
      claim_lease: 15m
```

One instance row per AWS account. Many instances supported; coordinator
handles them uniformly.

### Control Flow Per Claim

1. Worker receives claim `(instance, account, region, service)`.
2. Credentials: `sts:AssumeRole` using `role_arn` + `external_id`.
3. Scanner for `service` executes its `Scan` method, which:
   - issues `List*` and `Describe*` calls with throttling budget
   - resolves intra-service relationships (ECS service→task def, ALB
     listener→target group, Lambda alias→function)
   - streams `aws_resource`, `aws_relationship`, `aws_tag_observation`,
     `aws_image_reference`, `aws_dns_record` facts
   - applies redaction in the emission loop
4. On completion, worker reports resource counts, API call counts, throttle
   counts, and budget consumption to the coordinator.
5. Credentials are zeroed. Claim is released.

Cross-service dependencies (e.g., ECS service references a task def that
references an ECR image) are **not** resolved inside the scanner. Those
joins land in the correlation DSL, which already operates across fact
boundaries. The scanner emits flat, typed facts; the DSL joins them.

### Coordinator Completeness

The coordinator considers an AWS run complete when, for every
`(account, region, service_kind)` in the instance's configured set:

- the claim succeeded without `budget_exhausted`
- all declared pages were read or a checkpoint was committed
- no `assumerole_failed` warning is outstanding

Partial completion is a first-class state. A run where `ec2` succeeded but
`lambda` was budget-exhausted is explicitly `partial`, not `failed`, and
the next run resumes `lambda` from checkpoint.

---

## Invariants

After this collector lands, the following must hold:

1. The scanner holds no static AWS credentials.
2. Cross-account credentials live only for the duration of a single claim
   and are never persisted.
3. No plaintext environment variable value, secret field, or password
   attribute is emitted. Redaction is enforced in the emission path.
4. No S3 object contents, CloudWatch log contents, Secrets Manager secret
   values, or SSM parameter values are read. Metadata only, ever.
5. Every emitted fact carries `scope_id`, `generation_id`, `account_id`,
   `region`, `service_kind`, and `collector_instance_id`.
6. At most one worker is actively scanning a given `(account, region,
   service_kind)` tuple at a time. Throttle contention is coordinator-
   enforced, not prayer-driven.
7. Tags are emitted raw. The collector does not rename, alias, or normalize
   tag keys or values.
8. Claim ownership, fencing, and completeness flow through the workflow
   coordinator's shared contract.
9. The scanner does not write canonical graph truth. It does not correlate
   across accounts, across services outside its own scan, or with
   non-cloud sources.

---

## Observability Requirements

Metrics (prefix `eshu_dp_aws_`):

- `api_calls_total{service, account, region, operation}`
- `api_errors_total{service, account, region, error_class}`
- `throttle_total{service, account, region}`
- `scan_duration_seconds_bucket{service, account, region}`
- `claim_concurrency{account}` (gauge)
- `resources_emitted_total{service, account, region, resource_type}`
- `relationships_emitted_total{service, account, region}`
- `tag_observations_emitted_total{service, account, region}`
- `budget_exhausted_total{service, account, region}`
- `assumerole_failed_total{account}`
- `unchanged_resources_total{service, account, region}` (skipped emission)
- `pagination_pacing_events_total{service}`

Spans:

- `aws.collector.claim.process`
- `aws.credentials.assume_role`
- `aws.service.scan` (per service kind)
- `aws.service.pagination.page`
- `aws.fact.emit_batch`

Structured logs carry `scope_id`, `generation_id`, `account_id`, `region`,
`service_kind`, `collector_instance_id`. They must not carry tag values,
environment variable values, or secret-adjacent attribute values.

Admin status exposes, per instance:

- last successful scan per `(account, region, service_kind)`
- last throttle event and its counts
- outstanding `budget_exhausted` or `assumerole_failed` warnings
- per-account concurrency in-flight
- recent API error classes summarized

At 3 AM an operator should be able to answer: is the scanner stuck on
throttling, stuck on credentials, stuck on a budget ceiling, or simply
behind schedule.

---

## Explicit Non-Goals

1. Non-AWS clouds (GCP, Azure, Oracle, OCI) — separate ADRs per provider.
2. AWS data-plane content (S3 objects, log contents, Secrets Manager
   values).
3. Tag normalization, aliasing, or value canonicalization.
4. Drift detection between AWS and Terraform state — lives in the DSL.
5. Orphan detection, unmanaged-resource detection — lives in the DSL.
6. A second graph writer. The scanner emits facts; the reducer writes.
7. Replacing the coordinator's claim contract.
8. Long-lived or org-wide super credentials. Every account is a separate
   assume-role hop.
9. Cartography, Steampipe, or third-party CMDB adoption.

---

## Rollout Plan

### Phase 1: Runtime + IAM + Credentials + One Service (IAM)

- publish this ADR
- implement the runtime skeleton, claim loop, throttling, credentials
- ship IAM scanner first because it produces the trust-chain evidence that
  every other service needs, and because it has the most stable schema
- wire coordinator claim flow end to end, including completeness and
  partial-run semantics
- land core telemetry

### Phase 2: Container Vertical Slice (ECR + ECS + EKS + ELBv2 + Lambda + Route53 + EC2)

- add the remaining launch services
- validate the correlation DSL's container vertical slice against a real
  account with known state
- validate that throttling behavior at target concurrency is acceptable
  against a medium-sized org account

### Phase 3: Correlation DSL Joins

- image-to-code joins (ECR → build workflow → repo)
- ECS/EKS/Lambda → IAM role → code trust chain
- ALB listener + TargetGroup → ECS service → workload
- Route53 record → ALB/CloudFront → service
- drift joins against Terraform state facts (depends on state collector)

### Phase 4: Phase 2 Service Expansion

- RDS, DynamoDB, S3 (metadata), SQS, SNS, EventBridge
- CloudFront, API Gateway
- Secrets Manager, SSM Parameter Store (metadata)
- CloudWatch Logs (group metadata)

### Phase 5: Freshness Layer (Optional)

- EventBridge / AWS Config integration for change-driven refreshes
- retained only if it improves mean-time-to-observation without regressing
  correctness or cost

---

## Consequences

### Positive

- Platform gains cloud ground truth that no Git-only or state-only source
  can provide.
- Correlation DSL gains ARN-anchored joins across code, state, and cloud.
- Orphan, drift, and unmanaged detection become possible without inventing
  a second writer.
- Operators get one pane of glass for AWS observability under the existing
  coordinator status surface.

### Negative

- Introduces the first Eshu runtime that holds cross-account cloud
  credentials. Security review is mandatory at every phase.
- Adds per-service schema maintenance. AWS resource shapes evolve; the
  scanner must track SDK v2 updates.
- Introduces AWS API cost as a platform operational concern. Scan tuning
  moves from "nice to have" to "must have."

### Risks

- **Throttle-driven outages** of cloud control planes. Mitigated by
  coordinator-enforced claim uniqueness, per-service token buckets, adaptive
  retry, and per-account concurrency caps.
- **Credential compromise.** Mitigated by IRSA-only principals, required
  external IDs, claim-scoped credential lifetime, redaction of secret-
  adjacent attributes, and telemetry on `assumerole_failed`.
- **Schema drift** between SDK v2 and the scanner's typed fact shape.
  Mitigated by `unknown_resource_schema` warning facts and a versioned
  per-service schema package that is required to compile.
- **Silent under-scanning** when a high-cardinality service pages forever.
  Mitigated by hard API budgets per scan, resumable pagination checkpoints,
  and `budget_exhausted` warnings.

---

## Appendix: Implementation Workstreams

### Chunk A: Runtime Skeleton + IAM Scanner

**Scope:** runtime, claim loop, credentials, throttling, redaction, IAM
service scanner.

**Likely files:**

- `go/cmd/collector-aws-cloud/main.go`
- `go/internal/collector/awscloud/service.go`
- `go/internal/collector/awscloud/worker.go`
- `go/internal/collector/awscloud/credentials.go`
- `go/internal/collector/awscloud/throttle.go`
- `go/internal/collector/awscloud/services/iam/`

### Chunk B: Container Vertical Slice Services

**Scope:** ECR, ECS, EKS, ELBv2, Lambda, Route53, EC2.

**Likely files:**

- `go/internal/collector/awscloud/services/ecr/`
- `go/internal/collector/awscloud/services/ecs/`
- `go/internal/collector/awscloud/services/eks/`
- `go/internal/collector/awscloud/services/elbv2/`
- `go/internal/collector/awscloud/services/lambda/`
- `go/internal/collector/awscloud/services/route53/`
- `go/internal/collector/awscloud/services/ec2/`

### Chunk C: Coordinator Completeness + Admin Status

**Scope:** partial-run semantics, pagination checkpoints, admin status
surface parity with other collectors.

**Likely files:**

- `go/internal/workflow/...` (new in coordinator substrate)
- `go/internal/runtime/admin/...`
- `go/internal/collector/awscloud/service.go` checkpoints

### Chunk D: DSL Integration

**Scope:** correlation DSL rule packs consuming AWS facts. Depends on state
collector for drift rules, on Git collector for code joins.

**Likely files:**

- `go/internal/correlation/rules/aws_ecr/`
- `go/internal/correlation/rules/aws_ecs/`
- `go/internal/correlation/rules/aws_eks/`
- `go/internal/correlation/rules/aws_elbv2/`
- `go/internal/correlation/rules/aws_lambda/`
- `go/internal/correlation/rules/aws_route53/`
- `go/internal/correlation/rules/state_to_cloud_arn/`
- `go/internal/correlation/rules/cloud_drift/`

### Chunk E: Phase 2 Services

**Scope:** RDS, DynamoDB, S3 metadata, SQS, SNS, EventBridge, CloudFront,
API Gateway, Secrets Manager metadata, SSM metadata, CloudWatch Logs
metadata.

### Chunk F: Optional Freshness Layer

**Scope:** EventBridge / AWS Config integration. Defer entirely until phase
2 services are stable and operator demand is real.

---

## Recommendation

The platform should build a first-party AWS scanner against AWS SDK for Go
v2, with a worker pool claiming `(account, region, service_kind)` tuples
through the workflow coordinator. It should default to central STS
`AssumeRole` per claim with mandatory external IDs, while also supporting
account-local workload identity for stricter customer boundaries. It should
ship the container vertical slice at launch and emit raw tags for the
correlation DSL to normalize.

This runtime is the prerequisite for turning Terraform state and code intent
into anchored ground truth. Every cross-source canonical join the platform
wants to make — drift, orphan, unmanaged, workload placement, service
hostname evidence — becomes expressible the moment this collector emits
clean ARN-anchored facts.
