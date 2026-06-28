# perfcontract

## Purpose

`perfcontract` is the single in-code home for Eshu's published performance
thresholds and the lockstep gate that keeps them honest. It exists for B-5
(#3798): the three performance docs each state numbers, but only the hybrid gate
had an executable binding — the local envelope and reducer claim-latency numbers
were prose that an edit could change with nothing in code noticing.

The package encodes every documented threshold (`ContractThresholds`) and
`TestPerformanceContractMatchesDocs` reads the actual doc files and fails if a
threshold goes missing or its in-code value drifts from the documented token.
Because the standard `go test ./...` gate runs this package, CI fails on any
doc↔code performance-contract drift.

## What it covers

| Doc | Thresholds | How the runtime value is checked |
| --- | --- | --- |
| `local-performance-envelope.md` | cold start, warm restart, query p95s, single-file reindex, dead-code scan, reducer bulk-write | operator-gated (needs an active repo / 50K-fact load / consistent hardware) |
| `reducer-claim-latency-gate.md` | p95 ≤ 1.10× baseline; p95 increase ≤ 60s | operator-gated (needs a live Postgres benchmark at the documented depths) |
| `hybrid-retrieval-production-gate.md` | recall/precision/nDCG/p95/vector-coverage/false-canonical bars | the local-deterministic bars are measured by the hermetic `searchbench` gate; the production bars are operator-gated |

## Honesty boundary

This is a **contract** gate, not a runtime measurement. It guarantees the
documented numbers are real, present, and consistent with the code. Whether a
given build *meets* a threshold is measured where it can be measured honestly:
the hybrid local-deterministic bars by `searchbench` in hermetic CI, and the rest
by the operator/remote validation run on consistent hardware (see
[Local Performance Envelope](../../../docs/public/reference/local-performance-envelope.md)
and the `eshu-remote-validation` flow). The package deliberately does not
fabricate a measurement that hermetic CI cannot take.

## Ownership boundary

`perfcontract` owns only the threshold contract and its doc lockstep. It does not
run benchmarks, hold runtime state, or import service packages other than
`searchbench` (the single source of the hybrid numbers). The executable
evaluators it exposes (`ClaimLatencyContract.WithinBudget`) are pure functions
the operator run feeds real measurements into.
