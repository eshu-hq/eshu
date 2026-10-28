# ECS task count join theory (#9307)

A deployment story must count observed running ECS tasks, not configured
WorkloadInstance nodes or a service's desired count. The existing AWS cassette
contains an `ecs.service` with `attributes.cluster_arn` and an `ecs.task` with
the same cluster, `attributes.group=service:supply-chain-demo`, `state=RUNNING`,
and per-container image digests. A digest-only match cannot establish service
identity, and the CloudResource `running_image_digest` is empty for
multi-container tasks.

On 2026-10-28, a disposable PostgreSQL 18.6 container held 20,000 synthetic
`aws_resource` task facts, three in the target service and the rest in another
service. The read joined active-generation `fact_records` to
`ingestion_scopes` and `scope_generations`, selected `ecs.task` by exact
`attributes.cluster_arn` and `attributes.group`, filtered `state=RUNNING`,
and counted distinct task ARNs. `EXPLAIN (ANALYZE, BUFFERS)` returned three
rows and used a sequential scan without an ECS lookup index: 4.982 ms,
922 shared buffers. With a partial expression index on cluster and group for
active `ecs.task` facts, it used one index search: 0.055 ms, 13 hit and 2 read
buffers. This is a query-shape proof on synthetic data, not story-endpoint
latency or QA proof.

The production read must also authorize source scopes before counting, reject
stale/tombstoned facts, bound candidate work, distinguish no observed tasks
from no observation, and disclose which container digest corroborated an
image match. A multi-container task counts once by ARN. A task with an image
outside ECR needs an explicit skipped reason. None of those behaviors is
proved by this plan alone.

No-Regression Evidence: NOT_CHECKED; production code has not changed.
No-Observability-Change: the theory ran outside the API and adds no signal.

The new cassette reference uses the historical in-tree ECS scanner's
`runningContainerImageReferences` shape: a RUNNING ECR container with an
observed digest produces an `aws_image_reference` with repository, registry,
digest, and parsed tag. The existing distinct digest reference remains to
preserve prior identity assertions; two digests under one tag can be observed
during a rolling deployment. This fixture establishes matching digest input
only; it does not by itself prove the deployment-story read.
