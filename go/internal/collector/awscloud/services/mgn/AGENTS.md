# AGENTS.md - internal/collector/awscloud/services/mgn guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned MGN domain types.
3. `scanner.go` - application, source server, launch configuration, and job
   resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep MGN API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read or persist replication-agent credentials, replication
  configuration secrets, or replicated disk contents. Never call
  `GetReplicationConfiguration`, the replication-configuration-template reads,
  or any `Create*`, `Update*`, `Delete*`, `Start*`, `Stop*`, `Terminate*`,
  `Mark*`, `Initialize*`, `Finalize*`, or `Disconnect*` mutation API.
- The source-server node publishes its resource_id as the bare MGN source
  server id (fallback to ARN). Key the application-contains-source-server and
  job-targets-source-server edges on that exact bare id with no `target_arn` so
  they join the source-server node.
- The source-server-launched-EC2-instance edge targets `aws_ec2_instance` by the
  bare instance id (`i-...`) - a documented `relguard` forward reference until an
  EC2 instance scanner exists. Leave `target_arn` empty; read the instance id
  from the API, never synthesize it.
- The launch-configuration-uses-launch-template edge targets
  `aws_ec2_launch_template` by the launch template id (`lt-...`). The launch-
  config node and this edge share the synthesized id
  `<source-server-id>/launch-configuration`.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant or a documented `relguard` allowlist anchor
  and a `target_resource_id` matching how the target scanner publishes its
  resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from application, server, or
  job names, or AWS tags.
- Preserve stable application, source server, launch configuration, and job
  identities across repeated observations in the same AWS generation.
- Keep MGN resource ARNs, names, lifecycle state, tags, and AWS error payloads
  out of metric labels.

## Common Changes

- Add a new MGN metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry replication-agent credentials,
  replication secrets, or replicated disk contents, leave it out of the scanner
  contract.
- Add new relationship evidence only when the MGN API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (bare `i-` instance id, bare `lt-` launch template id, bare
  MGN source server id).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read replication configuration, replication templates, replication-
  agent credentials, or replicated disk contents, and do not call any MGN
  mutation or replication-control API.
- Do not synthesize an EC2 instance ARN or an EC2 launch template ARN; both
  targets are keyed by their bare AWS id.
- Do not resolve MGN names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
