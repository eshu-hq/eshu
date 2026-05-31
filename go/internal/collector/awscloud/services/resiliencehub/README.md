# resiliencehub scanner

Maps AWS Resilience Hub control-plane metadata into AWS cloud collector facts
for one claimed account and region. The scanner is metadata-only: it reads
describe/list APIs and never persists assessment result bodies, drift detail,
recommendation contents, or any data-plane payload.

## Resources

- `aws_resiliencehub_app` — application identity, status, compliance/drift
  labels, assessment schedule, configured RPO/RTO targets, resiliency score, and
  the integrated AppRegistry application ARN.
- `aws_resiliencehub_resiliency_policy` — policy identity, tier, estimated cost
  tier, data-location constraint, and per-failure-type (AZ/Hardware/Software/
  Region) RPO/RTO targets.
- `aws_resiliencehub_app_component` — application component name and type.
- `aws_resiliencehub_app_input_source` — the CloudFormation stack, Resource
  Group, AppRegistry application, Terraform state file, or EKS cluster the
  application draws its resources from (import type, source name, source ARN,
  reported resource count).
- `aws_resiliencehub_app_assessment` — assessment identity, status, compliance
  status, drift status, invoker, evaluated app version, and resiliency score.

## Relationships

- `resiliencehub_app_uses_policy` — app → resiliency policy, keyed by the policy
  ARN the policy node publishes (internal join).
- `resiliencehub_app_protects_resource` — app → a protected physical resource,
  keyed by the resource ARN the owning scanner publishes. Emitted only for the
  physical resources Resilience Hub identifies by an ARN whose owning scanner is
  also ARN-keyed (`aws_ecs_service`, `aws_efs_file_system`,
  `aws_elbv2_load_balancer`, `aws_lambda_function`, `aws_sns_topic`). Resilience
  Hub-native (non-ARN) physical identifiers are recorded only as app context and
  never keyed as an edge, so the graph never dangles.
- `resiliencehub_component_in_app` — component → app, keyed by the app ARN.
- `resiliencehub_input_source_in_app` — input source → app, keyed by the app
  ARN.
- `resiliencehub_assessment_for_app` — assessment → app, keyed by the app ARN
  Resilience Hub reports on the assessment summary.

## Versioned reads

Input sources, components, and protected physical resources are version-scoped.
The scanner reads the published `release` version. An application that has never
been published returns `ResourceNotFoundException` for those reads; the scanner
records a `resiliencehub_app_version_missing` warning and still emits the
application's summary metadata, rather than failing the whole scan.

## Partition awareness

The scanner never synthesizes an ARN. Every relationship ARN (policy ARN, app
ARN, protected-resource ARN) is the value AWS reports, so GovCloud (`aws-us-gov`)
and China (`aws-cn`) partitions are preserved and never rewritten to a literal
`arn:aws:`.

## Metadata-only guarantee

The accepted SDK surface is list/describe reads plus tag reads only, enforced by
a reflection guard test over the adapter `apiClient` interface. The scanner never
starts an assessment, imports resources, reads an assessment result or drift
detail, reads recommendation bodies, or mutates Resilience Hub state.

## Evidence

No-Regression Evidence: metadata-only control-plane scanner; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/resiliencehub/...` green.
No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.
