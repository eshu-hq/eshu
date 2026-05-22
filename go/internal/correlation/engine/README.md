# Correlation Engine

## Purpose

`correlation/engine` evaluates a validated rule pack against correlation
candidates and returns deterministic admission results for reducer and query
workflows.

## Ownership boundary

The engine owns rule ordering, candidate admission orchestration, rejection
reason attachment, tie-breaking by `CorrelationKey`, and final result ordering.
It does not define rule packs, candidate evidence, structural admission rules,
or explain rendering.

## Exported surface

Use `go doc ./internal/correlation/engine` for the godoc contract. The exported
surface is intentionally small:

- `Evaluate(pack rules.RulePack, candidates []model.Candidate)` validates the
  pack, sorts rules by `(Priority, Name)`, evaluates each candidate, dedupes
  admitted candidates by correlation key, and returns stable output.
- `Evaluation` carries aggregate match counts and result rows.
- `Result` carries candidate state, rejection reasons, and selected rule data.

## Dependencies

- `internal/correlation/rules` supplies rule-pack metadata.
- `internal/correlation/model` supplies candidates and evidence atoms.
- `internal/correlation/admission` owns confidence and structural gates.

## Telemetry

This package emits no telemetry. Reducer handlers that call it are responsible
for counters, spans, and status rows with bounded pack/rule labels.

## Gotchas / invariants

- Output ordering by `(CorrelationKey, State, ID)` is part of the contract.
  Tests and explain rendering rely on stable replay.
- Ties between admitted candidates sharing a `CorrelationKey` resolve by
  confidence and candidate ID. Do not make map iteration order observable.
- Structural or confidence failures must remain visible as rejection reasons;
  silently dropping candidates hides ambiguous evidence.
- The engine evaluates metadata rules. Value comparison, such as Terraform
  config/state drift classification, must happen before candidates enter here.

## Focused tests

```bash
go test ./internal/correlation/engine -count=1
go doc ./internal/correlation/engine
```

## Related docs

- `docs/public/reference/relationship-mapping.md`
- `go/internal/correlation/rules/README.md`
- `go/internal/correlation/admission/README.md`
