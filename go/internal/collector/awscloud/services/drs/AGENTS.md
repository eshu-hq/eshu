# AGENTS.md - internal/collector/awscloud/services/drs guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned DRS domain types.
3. `scanner.go` - source server, recovery instance, and template resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DRS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never install or read replication agent secrets. Never read replicated disk
  data, point-in-time snapshot contents, or job logs. Never call any `Recover*`,
  `Start*`, `Stop*`, `Reverse*`, `Terminate*`, `Create*`, `Update*`, or
  `Delete*` mutation API.
- The source server node publishes its resource_id as the source server id
  (fallback to the ARN). Key the source-server-to-recovery-instance edge on the
  recovery instance id so it joins the recovery instance node.
- The recovery instance node publishes its resource_id as the recovery instance
  id (fallback to the ARN). Key the recovery-instance-to-EC2-instance edge on
  the bare EC2 instance id (`i-...`) and leave `target_arn` empty;
  `target_type` is the documented `aws_ec2_instance` forward reference in
  `relguard.KnownTargetTypeAllowlist`.
- Do NOT synthesize a source-server-to-replication-config edge. The DRS API
  reports no template reference on a source server, so such an edge would
  dangle. Emit replication configuration templates as account-level resources
  only.
- Every relationship sets a non-empty `target_type` and a `target_resource_id`
  matching how the target node (or the forward-reference anchor) is keyed.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from server, instance, or
  template names, or AWS tags.
- Keep DRS resource ARNs, names, hostnames, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new DRS metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry an agent secret, replicated disk
  data, or snapshot content, leave it out of the scanner contract.
- Add new relationship evidence only when the DRS API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (or a documented forward-reference anchor).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read agent secrets, replicated disk data, snapshot contents, or job
  logs, or call any DRS recover/start/stop/mutation API.
- Do not resolve DRS names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
