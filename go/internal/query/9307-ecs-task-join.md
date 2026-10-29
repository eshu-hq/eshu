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

No-Regression Evidence: focused matrix and route tests pass locally. The
registry-selected static gates passed at `7e6b05a444`. Both the corrected full
golden replay and its retained-stack repeat passed 634 checks with zero required
failures and three advisory timing warnings at that SHA. Composed endpoint
latency remains NOT_PROVEN after an invalid quiet-host calibration.
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
not a stable latency improvement. Story-endpoint before/after latency
remains NOT_PROVEN: no accepted comparison exists.

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
against distinct running task ARNs.

The first full golden replay at `4c0728a98e` failed three exact pins: the new
task raised running-image nodes from two to three, and the Terraform declaration
raised IaC resources from 15 to 16 (total 24 to 25). The corrected snapshot
and synthetic counter passed full replay and a retained-stack repeat at
`7e6b05a444`: each had 634 passes, zero required failures, and three advisory
timing warnings. The registry-selected static gates also passed on that SHA.

The retained corpus was seeded once with 20,000 active ECS facts in a separate
account and scope. Its target service, task, and image-reference state was
byte-identical before and after seeding. Three unchanged-base calibration sets
ran, but timing-proof-rules §1 invalidated them because unrelated containers
were resident. The bounds were quarantined; no base/head endpoint comparison
has been accepted. Composed latency remains NOT_PROVEN.

A read-only QA check on 2026-10-28 found active typed AWS observations last
seen on 2026-10-26, older than active placeholder observations from
2026-10-28. Current extracted-collector typed freshness and the cutover
prerequisite remain NOT_CHECKED.

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
against a populated fact table. Disposable PostgreSQL 18.6 run
`eshu-9307-ecs-index-live-controlled-final` passed both live tests. Fresh
`ApplyBootstrap` left both indexes valid after 259 definitions. For an
upgrade, the test applied the first 257 definitions, seeded 30,000 AWS
resource facts, and held an uncommitted fact insert. While the tracked runner
applied 242/244, `pg_stat_progress_create_index` reported an active concurrent
build and a second distinct fact write committed under a 300 ms lock timeout.
The first writer remained open, so the build could not have completed before
the second write. After the first writer committed, the runner completed and
a full restart skipped all 259 receipts. A duplicate-key failure then left
an invalid same-name concurrent index; `ApplyBootstrap` rebuilt 242 with
`recovery=true`. An ordinary `CREATE INDEX` control waited for an ungranted
`ShareLock` on the same fact table, and its second writer failed with
SQLSTATE 55P03 under a 200 ms lock timeout. This RED control and concurrent
GREEN case prove writer admission during the actual tracked index build.
The host was contended by a separate pre-push run, so no durations from this
run are used as endpoint latency or stable speedup evidence.

On 2026-10-29, disposable run `eshu-9307-readjoin-final` re-ran the exact
shipped read (`ecsTaskReadAllScopesSQL` from `ecs_task_store.go`, no hand
copy) on PostgreSQL 18.6 with 30,003 active facts in one scope/generation
(one target service with `running_count=2`, two RUNNING tasks, one image
reference, 30,000 distractor service/task facts) after `ANALYZE`. Before
(both ECS indexes dropped): `EXPLAIN (ANALYZE, BUFFERS)` 14.229 ms, ~5,183
shared-buffer hits, service leg via the scope/generation keyset index
removing 15,002 rows, task leg filtering 30,002 rows; seven
`ListRunningTasks` rounds 0.0149-0.0182 s, median 0.017452 s. After
(migrations 242/244 re-applied from `MigrationSQL`): 0.187 ms, 12
shared-buffer hits, service leg on `fact_records_ecs_service_arn_idx`
(4 buffers) and task leg on
`fact_records_ecs_task_scope_cluster_group_idx` (2 rows in 0.012 ms,
4 buffers); seven rounds 0.00045-0.00381 s, median 0.000559 s. Same rows
both sides. Planner statistics are load-bearing: without a preceding
`ANALYZE`, the task leg stays on the keyset index with a 30k-row filter
(7.918 ms, 2,979 buffers) while the service leg still uses the ARN index;
correctness is identical, only the plan is slower, and production
autovacuum provides the statistics. 16-CPU disposable host:
load1 7.53 at start (below the 8.0 half-CPU bound) but 10.45 at end, with
four idle sibling containers resident that this run must not disturb, so
per timing-proof-rules §1 the wall times are corroboration with spread,
not gated claims; the deterministic evidence is the plan node set, the
index names, the buffer counts, and the row counts. This is the read-time
join and the store read the story endpoint runs; a composed HTTP
endpoint comparison with a live full stack remains NOT_PROVEN (handler
tests use graph/content fakes).
