# AGENTS.md - internal/collector/awscloud/services/shield guidance

## Read First

1. `README.md` - package purpose, exported surface, classifier table, and
   invariants.
2. `types.go` - scanner-owned Shield domain types.
3. `helpers.go` - the protected-ARN-to-target-type classifier.
4. `relationships.go` - protection-to-protected-resource edge construction.
5. `scanner.go` - resource and relationship emission.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-security.md` - Shield data
   boundaries and security review requirements.

## Invariants

- Keep Shield API access behind `Client`; do not import the AWS SDK into this
  package.
- Metadata only. Never read or persist subscription limits, time commitment,
  start/end times, proactive engagement status, emergency contacts, or any
  other billing detail beyond the subscription state and auto-renew flag.
- Every emitted relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its `resource_id`. Skip emission for an unrecognized
  protected ARN service; never emit an empty or guessed `target_type`.
- The protected resource ARN comes from the API already partition-correct. Use
  it directly (or extract its bare id); never synthesize it or hardcode
  `arn:aws:`. If a protection ARN must ever be synthesized, derive the partition
  with `awscloud.PartitionForBoundary` / `PartitionFromARN`.
- For an ARN-keyed target (ELBv2, CloudFront, Global Accelerator) set both
  `target_arn` and the ARN-shaped `target_resource_id`. For a bare-id target
  (Elastic IP, hosted zone) set only the bare `target_resource_id` and leave
  `target_arn` unset, or the relguard join-mode check fails.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from protection names.
- Keep ARNs, ids, and reference values out of metric labels.

## Common Changes

- Add a new protected-resource family by extending the `classifyProtectedARN`
  switch in `helpers.go`, writing a focused scanner test first that asserts the
  new `target_type` and `target_resource_id` match the target scanner's
  published `resource_id`. Add the runtime relguard assertion for the new edge.
- Add a new metadata field by extending the scanner-owned type and mapping it
  through `awscloud` envelope builders. Never add a billing field.
- Extend SDK pagination and region pinning in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not persist any subscription billing detail.
- Do not emit a relationship with an empty, guessed, or non-constant
  `target_type`.
- Do not resolve protection names into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
