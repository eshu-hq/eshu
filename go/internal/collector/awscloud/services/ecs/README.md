# AWS ECS Scanner

## Purpose

`internal/collector/awscloud/services/ecs` owns the ECS scanner contract for
the AWS cloud collector. It converts clusters, services, task definitions,
tasks, service load-balancer bindings, task-definition container image
relationships, and running-task container `aws_image_reference` facts into AWS
cloud fact envelopes.

## Ownership boundary

This package owns scanner-level ECS fact selection, task-definition redaction,
and identity mapping. It does not own AWS SDK pagination, STS credentials,
workflow claims, fact persistence, graph writes, reducer admission, or query
behavior.

```mermaid
flowchart LR
  A["ECS API adapter"] --> B["Client"]
  B --> C["Scanner.Scan"]
  C --> D["aws_resource"]
  C --> E["aws_relationship"]
  C --> G["aws_image_reference"]
  D --> F["facts.Envelope"]
  E --> F
  G --> F
```

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal ECS read surface consumed by `Scanner`.
- `Scanner` - emits ECS resource and relationship envelopes for one boundary.
- `Cluster`, `Service`, `TaskDefinition`, and `Task` - scanner-owned ECS
  resource representations.
- `Container`, `EnvironmentVariable`, `SecretReference`, `LoadBalancer`,
  `TaskContainer`, and `TaskNetworkInterface` - scanner-owned nested ECS
  records.
- `parseECRImage` - parses a running container's `image` string into an ECR
  registry id, repository name, and tag when the host matches the ECR
  registry shape.

## Dependencies

- `internal/collector/awscloud` for boundaries, resource constants,
  relationship constants, and envelope builders.
- `internal/facts` for emitted fact envelope kinds.
- `internal/redact` for HMAC-SHA256 task-definition environment value markers.

The package depends on a small `Client` interface rather than the AWS SDK for
Go v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource/relationship counts after
`Scanner.Scan` returns. The `awssdk` adapter records ECS API call counts,
throttles, and pagination spans.

## Gotchas / invariants

- ECS task-definition environment values are always replaced with
  `redacted:hmac-sha256:` markers before persistence.
- ECS secret `value_from` references are preserved because they are ARNs or
  provider references, not secret values.
- ECS service-to-task-definition, task-definition-to-image,
  service-to-load-balancer, and task-to-ENI bindings are emitted as
  `aws_relationship` facts.
- ECS task ENI details are reported attachment evidence used by later reducers
  to join tasks to EC2 subnet and VPC topology.
- Task-definition container images are relationship targets
  (`RelationshipECSTaskDefinitionUsesImage`), not `aws_resource` facts, in this
  package.
- Every RUNNING task container with a non-blank `ImageDigest` and an
  ECR-hosted `Image` (host matches `<registry-id>.dkr.ecr.<region>.amazonaws.com`,
  including the `.amazonaws.com.cn` partition) emits a first-class
  `aws_image_reference` fact (#5451). This is the strongest available
  deployed-code signal ECS offers: the digest DescribeTasks reports for the
  container running right now, not a build-time or task-definition reference.
  It lets the digest-keyed `container_image_identity` reducer resolve a
  running task straight to the repository and commit that built its image,
  which reading only the task's `aws_resource` `containers[]` attribute never
  did (that attribute is not read by the identity resolver at all).
- A non-ECR running image (`docker.io`, `ghcr.io`, a private registry, ...) is
  a bounded gap: `aws_image_reference` models an AWS account/region/repository
  shape that a non-ECR image does not fit, so the scanner intentionally does
  not force it and emits no image reference for that container. The image is
  still visible through the task's `aws_resource` `containers[]` attribute and
  the task-definition's `RelationshipECSTaskDefinitionUsesImage` relationship;
  only the digest-keyed identity join is unavailable for it.
- A non-RUNNING task (STOPPED, PENDING, ...) never emits an image reference —
  only the RUNNING state is the deployed-code signal this fact exists to
  carry.
- The scanner stops on client errors. Runtime adapters decide whether an AWS
  service error is retryable, terminal, or a warning fact.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
