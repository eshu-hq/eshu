# internal/correlation/drift/tfconfigstate Agent Rules

This package is deterministic helper Go for the
`terraform_config_state_drift` rule pack. It classifies config/current-state/
prior-state rows and builds candidates; it MUST NOT own reducer queues,
telemetry emission, graph projection, or cloud joins.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `drift_kind.go`, `classify.go`, `candidate.go`,
   `attribute_allowlist.go`, and focused tests.
3. `../../rules/terraform_config_state_drift_rules.go`.
4. `docs/public/reference/local-testing.md` drift proof gates.

## Local Invariants

- `Classify` MUST be exclusive: at most one `DriftKind` per address.
- Dispatch order is load-bearing: lineage rotation suppression,
  `removed_from_state`, `removed_from_config`, `added_in_state`,
  `added_in_config`, then `attribute_drift`.
- `removed_from_config` requires `PreviouslyDeclaredInConfig`; raw state-only
  presence is not enough.
- Unknown/computed config attributes MUST suppress `attribute_drift` for that
  path.
- `BuildCandidates` MUST sort by address for deterministic candidate IDs and
  explain traces.
- Every emitted candidate MUST carry `EvidenceTypeDriftAddress`; the rule pack
  structural gate depends on it.
- Config evidence uses the config anchor scope; state and prior evidence use
  the state snapshot scope.
- High-cardinality addresses, module paths, and attribute paths MUST stay out of
  metric labels.

## Change Rules

- New drift kind: update enum, classifier, candidate evidence if needed, rule
  docs, telemetry cardinality proof, and positive/negative/ambiguous fixtures.
- Allowlist additions MUST stay small and high-signal; run allowlist and corpus
  tests.
- Candidate evidence changes MUST keep
  `rules.TerraformConfigStateDriftRulePack` structural selectors aligned.
- Do not move value comparison into `correlation/rules`; the DSL does not
  compare values.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/drift/tfconfigstate -count=1
go vet ./internal/correlation/drift/tfconfigstate
go doc ./internal/correlation/drift/tfconfigstate
```

Classifier changes require positive, negative, and ambiguous fixture proof plus
`go test ./internal/correlation/... -count=1`. Docs-only edits also need the
package-doc verifier for this directory and `git diff --check`.
