# AGENTS — cloudruntime

## Read first

1. `doc.go` — package contract.
2. `classify.go` — exclusive orphan/unmanaged dispatch.
3. `candidate.go` — ARN-keyed candidate and evidence construction.
4. `telemetry.go` — bounded metric emission.
5. `../../rules/aws_cloud_runtime_drift_rules.go` — rule-pack declaration.
6. `docs/docs/adrs/2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
   — cloud observation joins phase.

## Invariants

- `Classify` returns at most one `FindingKind` per ARN.
- Cloud-only resources are orphaned before unmanaged can be considered.
- Unmanaged requires AWS cloud and Terraform state evidence but no current
  Terraform config evidence.
- `BuildCandidates` must emit an `EvidenceTypeCloudResourceARN` atom or the
  rule pack's structural gate rejects the candidate.
- Raw tag evidence stays in `EvidenceAtom`s only. Do not add tag keys, tag
  values, ARNs, Terraform addresses, or account IDs as metric labels.

## Common changes

- Add a finding kind: update `FindingKind`, `Classify`, `BuildCandidates`
  evidence, `RecordEvaluation`, tests, telemetry docs, and the active ADR.
- Change candidate evidence shape: keep `EvidenceTypeCloudResourceARN` aligned
  with `rules.AWSCloudRuntimeDriftRulePack`.
- Add reducer wiring: keep loaders and graph publication outside this package.
  This package should stay a deterministic classifier and candidate builder.

## Failure modes

- No orphan/unmanaged counters: check that callers ran `engine.Evaluate` with
  `rules.AWSCloudRuntimeDriftRulePack()` and then called `RecordEvaluation`.
- Structural mismatches: check for a missing `aws_cloud_resource_arn` evidence
  atom.
- False unmanaged findings: verify the loader joined current Terraform config
  by ARN before constructing `AddressedRow`.

## Anti-patterns

- Do not query Postgres or graph backends from this package.
- Do not infer environment or service ownership from tag names here. Tags are
  raw evidence for a later normalization rule.
- Do not add backend-specific NornicDB or Neo4j branches.

## What NOT to change without an ADR

- The ARN-primary join contract.
- The exclusive orphan-before-unmanaged dispatch order.
- The metric label set for orphan and unmanaged counters.
