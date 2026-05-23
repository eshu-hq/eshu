# cloudruntime

## Purpose

`cloudruntime` contains the helper Go for the `aws_cloud_runtime_drift` rule
pack. It classifies AWS-observed resources against Terraform state and
Terraform config views by ARN, then builds `model.Candidate` values for the
correlation engine.

## Ownership boundary

Owns the AWS runtime drift classifier, candidate evidence shape, and telemetry
helper for admitted orphaned and unmanaged findings. It does not query
Postgres, write Cypher, publish graph phase rows, or decide deployment truth.

## Exported surface

- `FindingKind` and its two values in `classify.go`.
- `ResourceRow` and `Classify` in `classify.go`.
- `AddressedRow`, `BuildCandidates`, and evidence constants in `candidate.go`.
- `Summary` and `RecordEvaluation` in `telemetry.go`.

See `doc.go` for the godoc contract.

## Dependencies

- `go/internal/correlation/model` for candidates and evidence atoms.
- `go/internal/correlation/engine` for telemetry over evaluated results.
- `go/internal/correlation/rules` for the AWS rule-pack name and rule names.
- `go/internal/telemetry` for bounded metric labels.

## Telemetry

`RecordEvaluation` emits:

- `eshu_dp_correlation_rule_matches_total{pack, rule}`
- `eshu_dp_correlation_orphan_detected_total{pack, rule}`
- `eshu_dp_correlation_unmanaged_detected_total{pack, rule}`

ARNs, Terraform addresses, and tag values stay out of metric labels. They live
only in evidence atoms for later explanation or structured logs.

## Gotchas / invariants

- ARN is the primary join key. `BuildCandidates` sorts by ARN so explain traces
  remain stable across reducer reruns.
- Classification is exclusive. Cloud-only resources are `orphaned_cloud_resource`;
  cloud plus state with no config is `unmanaged_cloud_resource`; unresolved
  collector/config coverage becomes `unknown_cloud_resource`; conflicting
  deterministic owner evidence becomes `ambiguous_cloud_resource`.
- Cloud plus state plus config produces no candidate because the three source
  layers converge for this slice.
- Raw AWS tags become `aws_raw_tag` evidence with keys like `tag:Environment`.
  Collectors must not turn tag names into platform or environment truth.

## Related docs

- `go/internal/correlation/rules/README.md`
- `docs/docs/adrs/2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md`
