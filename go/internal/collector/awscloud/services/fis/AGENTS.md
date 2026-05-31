# AGENTS.md - internal/collector/awscloud/services/fis guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned FIS domain types.
3. `scanner.go` - experiment-template resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware bucket ARN synthesis,
   ARN parsing, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep FIS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never start or stop an experiment, never read experiment run results
  (`GetExperiment`, `ListExperiments`, `ListExperimentResolvedTargets`), and
  never call any `Create*`, `Update*`, `Delete*`, `Start*`, `Stop*`, or `Tag*`
  mutation API. The accepted read surface is `ListExperimentTemplates`,
  `GetExperimentTemplate`, and `ListTagsForResource` only.
- Never persist action parameter values, target filter values, or target-tag
  selectors. They can carry secret or payload-shaped data; only the action id,
  resource-type selector, selection mode, and explicit resource ARNs are
  metadata.
- The experiment-template node publishes its resource_id as the template ARN
  (fallback to the template id). Source every template edge on that value.
- Emit the template-to-IAM-role edge only when FIS reports an ARN-shaped role;
  the role ARN matches the IAM scanner's published role resource_id.
- Type each explicit target ARN to its resource family and key it to that
  family's published identity: EC2 instances by the bare `i-` id (forward-
  reference `aws_ec2_instance`, with `target_arn` left empty and the full ARN
  in the `instance_arn` edge attribute so relguard does not reject a bare-id
  join key paired with an ARN), ECS clusters and RDS
  DB instances/clusters by ARN. Skip targets selected only by tag/filter and
  ARNs from unmodeled families - never key a dangling edge.
- Trim the trailing `:*` wildcard from the reported CloudWatch log group ARN so
  the log-group edge joins the cloudwatchlogs node.
- Emit the template-to-S3 edge only when an S3 log destination is configured.
  FIS reports a bucket NAME, so synthesize the bucket ARN with
  `awscloud.PartitionForBoundary` and never hardcode `arn:aws:` - GovCloud and
  China must resolve to the real bucket node.
- Emit the stop-condition edge only for `aws:cloudwatch:alarm` conditions whose
  Value is an alarm ARN; the implicit `none` condition emits no edge.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant or a documented `relguard`
  `KnownTargetTypeAllowlist` anchor, and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from template names or AWS
  tags.
- Keep FIS ARNs, names, descriptions, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new FIS metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry parameter, filter, or run-output
  values, leave it out of the scanner contract.
- Add new relationship evidence only when the FIS API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not start, stop, or mutate experiments, read experiment runs, or call any
  FIS mutation API.
- Do not persist action parameters, target filters, or target tags.
- Do not resolve FIS names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
