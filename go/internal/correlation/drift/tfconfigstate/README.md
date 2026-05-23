# tfconfigstate

## Purpose

`tfconfigstate` implements the helper code for the
`terraform_config_state_drift` correlation rule pack. It classifies one
Terraform resource address across config, current state, and prior state views,
then builds cross-scope candidates for reducer admission.

## Ownership boundary

This package owns the five drift classifiers, the compile-time attribute
allowlist, stable drift evidence tokens, and deterministic candidate building.
It lives beside `correlation/rules` to avoid a circular import: `rules` declares
the pack metadata, while this package performs value comparison before
`engine.Evaluate(rules.TerraformConfigStateDriftRulePack(), ...)` runs.

It does not own reducer queue handling, telemetry emission, graph projection, or
state-to-cloud ARN joins.

## Exported surface

Use `go doc ./internal/correlation/drift/tfconfigstate` for the complete
godoc contract. The maintainer-facing surface is:

- `DriftKind`, `AllDriftKinds`, and `DriftKind.Validate`.
- `ResourceRow` and `Classify` for config/state/prior comparison.
- `AddressedRow` and `BuildCandidates` for deterministic candidate emission.
- `AllowlistFor` and `AllowlistResourceTypes` for v1 attribute drift policy.
- Evidence type/key constants consumed by the rule pack structural gate and
  explain trace.

## Dependencies

- `internal/correlation/model` supplies `Candidate` and `EvidenceAtom`.
- `internal/correlation/rules` supplies the drift rule-pack name.
- `internal/relationships/tfstatebackend` supplies config-side scope identity.

## Telemetry

This package emits no telemetry directly. The reducer handler that consumes its
output emits `eshu_dp_correlation_rule_matches_total{pack, rule}` and
`eshu_dp_correlation_drift_detected_total{pack, rule, drift_kind}`.

Keep resource addresses, attribute paths, and module paths out of metric labels;
they belong in structured logs or explain evidence.

## Gotchas / invariants

- Unknown or computed config values must appear in
  `ResourceRow.UnknownAttributes`; otherwise `attribute_drift` can compare an
  expression token against a concrete state value.
- `removed_from_state` requires a prior-state row and is suppressed by lineage
  rotation.
- `removed_from_config` requires `PreviouslyDeclaredInConfig`; raw state-only
  presence is not enough.
- `Classify` dispatch order is load-bearing: the stronger
  `removed_from_config` signal runs before `added_in_state`.
- `BuildCandidates` sorts by address so explain traces are stable across
  reducer reruns.
- The v1 allowlist is code-owned in `attribute_allowlist.go`. Moving it to data
  requires architecture-owner approval and focused drift evidence.

## Focused tests

```bash
go test ./internal/correlation/drift/tfconfigstate -count=1
go doc ./internal/correlation/drift/tfconfigstate
```

## Related docs

- `docs/public/reference/local-testing.md`
- `docs/public/reference/relationship-mapping.md`
