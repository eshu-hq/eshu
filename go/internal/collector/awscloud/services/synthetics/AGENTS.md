# AGENTS.md - internal/collector/awscloud/services/synthetics guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Synthetics domain types.
3. `scanner.go` - canary resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, artifact-bucket-name extraction,
   partition-aware bucket ARN synthesis, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Synthetics API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist canary script source code (handler, source location,
  zip file), run artifacts (logs, screenshots, HAR files), or run results. Never
  call `GetCanaryRuns`, `DescribeCanariesLastRun`, `GetCanary` (code read), or
  any `Create*`, `Update*`, `Delete*`, `Start*`, `Stop*` mutation/control API.
- `DescribeCanaries` returns no ARN, so the adapter synthesizes the canary ARN
  with `awscloud.PartitionForBoundary` and never hardcodes `arn:aws:`. The
  canary node publishes that ARN as its resource_id (fallback to name); source a
  canary's own edges on that same value.
- Emit the canary-to-S3 edge only when an artifact location is reported.
  Synthetics reports a `bucket/prefix` location PATH, not an ARN. Extract the
  leading bucket-name segment, then synthesize the partition-aware bucket ARN so
  it joins the S3 scanner's published bucket node in every partition.
- Key the canary-to-IAM-role edge on the execution role ARN, matching the IAM
  scanner's published role resource_id.
- Key the canary-to-subnet and canary-to-security-group edges on the BARE
  `subnet-...` and `sg-...` ids the EC2 scanner publishes; never an ARN. Emit
  them only when the canary is VPC-configured.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from canary names or AWS tags.
- Preserve stable canary identities across repeated observations in the same AWS
  generation.
- Keep Synthetics ARNs, names, schedules, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Synthetics metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry script source, run
  artifacts, or run results, leave it out of the scanner contract.
- Add new relationship evidence only when DescribeCanaries reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for IAM roles and S3 buckets, bare ids for
  subnets and security groups).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read canary code, run artifacts, or run results; do not call any
  Synthetics mutation or run-control API.
- Do not resolve Synthetics names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
