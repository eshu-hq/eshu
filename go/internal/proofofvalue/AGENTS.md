# AGENTS.md — internal/proofofvalue guidance for LLM assistants

## Read first

1. `go/internal/proofofvalue/README.md` — purpose, honesty boundary, pieces.
2. `go/internal/proofofvalue/doc.go` — package contract.
3. `go/internal/proofofvalue/score.go` — `Score`, metrics, delta.
4. `go/internal/proofofvalue/baseline.go` — the grep-only verdict model.
5. `go/internal/proofofvalue/harness.go` — `BuildRun` corpus wiring.
6. `go/cmd/proof-of-value/` — the operator runner that emits the artifact.

## Invariants this package enforces

- The scorer computes nothing favorable on its own. Every number comes from
  supplied predictions versus supplied ground truth. Do not add heuristics that
  adjust, smooth, or bias scores toward either strategy.
- Misses must stay visible. `DeadFalseNegative` and `DeadFalsePositive` are part
  of the contract. Do not collapse them into accuracy.
- The baseline must remain a *faithful* grep model, not a strawman. It may only
  use literal text matching over corpus files. Do not give it graph knowledge,
  family-specific reference semantics, or an "ambiguous" verdict.
- The Eshu strategy must call the real analyzer (`internal/iacreachability`).
  Do not hardcode Eshu answers; that would fabricate the delta.
- Invalid input fails loudly. Keep the validation in `validate.go` strict.

## When you change this package

- Add or update failing-first tests in `score_test.go` (pure logic) before
  changing scoring. Keep `harness_test.go` running against the real fixture
  corpus so the measured delta stays honest.
- If you extend the harness to a new corpus (for example call-graph or
  correlation fixtures), add a new `BuildRun`-style adapter rather than
  weakening the IaC one. Each adapter must derive ground truth from a curated
  fixture truth file, not from the tool being measured.
- Keep files under 500 lines and update `README.md`, `doc.go`, and this file
  when the contract changes.

## Verification

```bash
cd go && go test ./internal/proofofvalue ./cmd/proof-of-value -count=1
cd go && gofmt -l internal/proofofvalue cmd/proof-of-value
cd go && golangci-lint run ./internal/proofofvalue/... ./cmd/proof-of-value/...
scripts/verify-package-docs.sh
```
