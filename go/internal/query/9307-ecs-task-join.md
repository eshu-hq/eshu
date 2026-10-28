# ECS task count join theory (#9307)

A deployment story must count observed running ECS tasks, not configured
WorkloadInstance nodes or a service's desired count. The existing AWS cassette
contains an `aws_ecs_service` with `attributes.cluster_arn` and an `aws_ecs_task` with
the same cluster, `attributes.group=service:supply-chain-demo`, `state=RUNNING`,
and per-container image digests. A digest-only match cannot establish service
identity, and the CloudResource `running_image_digest` is empty for
multi-container tasks.

On 2026-10-28, disposable run `eshu-9307-theory` used a PostgreSQL 18.6
container holding 20,000 synthetic
`aws_resource` task facts, three in the target service and the rest in another
service. The read joined active-generation `fact_records` to
`ingestion_scopes` and `scope_generations`, selected `aws_ecs_task` by exact
`attributes.cluster_arn` and `attributes.group`, filtered `state=RUNNING`,
and counted distinct task ARNs. `EXPLAIN (ANALYZE, BUFFERS)` returned three
rows and used a sequential scan without an ECS lookup index: 4.982 ms,
922 shared buffers. With a partial expression index on cluster and group for
active `aws_ecs_task` facts, it used one index search: 0.055 ms, 13 hit and 2 read
buffers. This is a query-shape proof on synthetic data, not story-endpoint
latency or QA proof.

The production read must also authorize source scopes before counting, reject
stale/tombstoned facts, bound candidate work, distinguish no observed tasks
from no observation, and disclose which container digest corroborated an
image match. A multi-container task counts once by ARN. A task with an image
outside ECR needs an explicit skipped reason. None of those behaviors is
proved by this plan alone.

No-Regression Evidence: focused matrix and route tests pass locally; full endpoint latency and live corpus proof remain NOT_CHECKED.
Observability Evidence: the new impact.ecs_running_task_count span records observed count or a skip reason; the query read also uses the existing guarded Postgres read port.

The new cassette reference uses the historical in-tree ECS scanner's
`runningContainerImageReferences` shape: a RUNNING ECR container with an
observed digest produces an `aws_image_reference` with repository, registry,
digest, and parsed tag. The existing distinct digest reference remains to
preserve prior identity assertions; two digests under one tag can be observed
during a rolling deployment. This fixture establishes matching digest input
only; it does not by itself prove the deployment-story read.

The production-shaped service-ARN CTE and same-generation lateral task read
were then run against the same disposable 20,001-fact set. With no ECS indexes,
PostgreSQL used two fact-record sequential scans: 8.545 ms and 1,827 shared
buffers. With partial service-ARN and scope/generation/cluster/group indexes,
PostgreSQL used two index scans: 0.074 ms and 9 shared buffers. External host
contention was present; these numbers establish the plan shape and cardinality,
not a stable latency improvement. Story-endpoint before/after latency remains
NOT_CHECKED.

The original cassette used legacy `ecs.*` resource-type strings. Historical
in-tree `serviceObservation` and `taskObservation` pass
`catalog.ResourceTypeECSService` and `catalog.ResourceTypeECSTask` into
`NewResourceEnvelope`, which copies them into the payload unchanged. Those
constants are `aws_ecs_service` and `aws_ecs_task`; the fixture and read
predicates now use the producer's actual types. The first SQL plan comparison used equivalent synthetic legacy type labels.
A second disposable PostgreSQL 18.6 transaction updated all 20,001 synthetic
facts to the production `aws_ecs_service` / `aws_ecs_task` type strings and ran
the exact service-ARN CTE plus same-generation lateral task read. Without the
new indexes it returned three task rows using two fact-record sequential scans
in 8.618 ms with 3,641 shared-buffer hits. With the exact migration indexes it
returned the same three rows using the service-ARN and task
scope/generation/cluster/group indexes in 0.069 ms with six buffer hits and
three reads. The transaction rolled back. Host contention makes these plan and
cardinality evidence, not a stable endpoint speedup claim.

The workload binding selected for this change is a read-time exact declaration:
`deployable-source/main.tf` declares `aws_ecs_service.deployable_source` with
literal `name=deployable-source` and a full cluster ARN. The parser emits a
`TerraformResource` content entity with those `attributes`. The trace handler
uses that declaration only if the selected canonical workload name matches,
the defining repository is granted and defines exactly one workload, and the
resource has no `count`, `for_each`, or computed attributes. It derives the
service ARN from the literal cluster and name, then requires a current
same-scope AWS service fact with that exact ARN and a matching task group and
cluster. A second service in the same AWS scope cannot join by shared image
digest or by a request name. The state cassette's module-prefixed ECS address
is not used as a workload binding because `MATCHES_STATE` deliberately refuses
normalized module addresses.

The golden corpus now has a separate `deployable-source` ECS service, running
task, and matching `aws_image_reference`, leaving the older
`supply-chain-demo` service and reference assertions intact. The HTTP
`deployable-source` shape and an additional MCP `?assert=ecs_runtime` shape
require the observed count, source, evidence tier, and matching container.
The source value names both AWS `DescribeServices` and `DescribeTasks`, because
zero is based on the service's observed running count while nonzero is checked
against distinct running task ARNs. Full golden replay and endpoint latency
are still NOT_CHECKED pending host coordination.

Historical scanner envelope parity was checked on this pre-extraction branch with
`cd go && go test ./internal/collector/awscloud/service/ecs -run
'TestScannerEmitsECSResourcesWithRedactedTaskDefinitionEnvironment|TestScannerEmitsImageReferenceForRunningECRContainer|TestScannerSkipsImageReferenceForNonECRImage'
-count=1 -v` (exit 0). The service and task test calls `Scanner.Scan` and
`assertResourceType` on emitted envelope payloads, not just catalog constants;
the image test checks the emitted envelope's account, region, repository,
registry, digest, manifest digest, and tag. The non-ECR test confirms no
reference is emitted. This is local historical producer parity only; QA's
extracted collector remains NOT_CHECKED.

The two ECS read indexes are migration-only, each in a sole
`CREATE INDEX CONCURRENTLY` statement. `EnsureSchema` does not build them
against a populated fact table. On a disposable PostgreSQL 18.6 database,
`TestECSRuntimeIndexFreshBootstrapLive` passed with both indexes valid after
the complete 259-definition bootstrap. `TestECSRuntimeIndexUpgradeLive`
applied the first 257 definitions, seeded 30,000 AWS resource facts, then
applied 240-241 through the tracked runner while an ordinary insert/delete
writer stayed active. Atomic snapshots immediately around that two-index call
recorded 71 completed cycles in its 108.781 ms window; the test requires at
least two completions so at least one full insert/delete pair occurs inside
the call. The runner logged concurrent index builds of 86 ms and 17 ms. A
full restart skipped all 259 receipts. After a duplicate-key
failure left an invalid same-name concurrent index, a second full
`ApplyBootstrap` rebuilt 240 with `recovery=true`. Both tests exited 0.
These timings describe this disposable run under shared-host load; they do
not establish composed endpoint latency or a stable before/after speedup.
