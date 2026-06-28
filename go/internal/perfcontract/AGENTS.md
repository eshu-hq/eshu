# AGENTS.md — internal/perfcontract guidance for LLM assistants

## Read first

1. `go/internal/perfcontract/README.md` — purpose, coverage, and the honesty
   boundary.
2. `go/internal/perfcontract/contract.go` — `Threshold`, `Enforcement`, and
   `ContractThresholds`.
3. `go/internal/perfcontract/contract_test.go` — the doc↔code lockstep gate.

## Invariants

- Every threshold in the three performance docs MUST have a `Threshold` entry,
  and every entry's `Phrase` MUST appear verbatim in its `Doc`. If you change a
  number, change the doc, the `Phrase`, the `Token`, and the `Value` together —
  `TestPerformanceContractMatchesDocs` fails otherwise.
- Do NOT add a `Threshold` claiming `EnforcementHermeticGate` unless a real
  hermetic, credential-free gate measures it (today only the `searchbench`
  local-deterministic bars qualify). Everything else is
  `EnforcementOperatorGated`. Never fabricate a measurement hermetic CI cannot
  take — that violates the repo's accuracy rule.
- The hybrid numbers come from `searchbench.ProductionGateThresholdsFor` so there
  is one in-code source. Do not hardcode duplicate hybrid values here.

## When you touch a performance doc

If you change a documented threshold, update the matching `Threshold` entry in
this package in the same change. The lockstep test is the gate that makes the doc
and the code move together.
