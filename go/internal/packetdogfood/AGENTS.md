# AGENTS: internal/packetdogfood

Scope: the evidence-packet dogfood benchmark scorer (issue #3143).

## Rules

- Keep this package pure and deterministic. No graph, content, provider,
  network, filesystem, or clock reads. It scores a captured benchmark artifact
  and nothing else. Capture happens elsewhere (operator run or fixture).
- The scoring contract is the public surface: `Benchmark`, `Task`,
  `ApproachResult`, `Verdict`, `Criterion`, `ParseBenchmark`, and `Score`. Treat
  the JSON field names as a wire contract; changing them is a breaking change
  that must update `testdata/fixture_benchmark.json`, the CLI, and the public
  reference doc in lockstep.
- A change to a scoring rule MUST update the docstring, the README scoring list,
  and the public reference doc, and MUST keep `grounding_test.go` green so the
  fixture's packet token budgets stay achievable by the real emitters.
- The benchmark must keep covering all of `requiredFamilies` (supply-chain
  impact, deployable drift, service context). Do not weaken a criterion to make
  a run pass; fix the captured artifact or the emitter instead.
- `requiredFamilies`, the `Approach` constants, and `BenchmarkSchema` are the
  closed vocabularies. Add a new approach or family only with a corresponding
  scoring rule and test.
- The gate proves the packet beats the BEST (fastest, smallest) baseline, so a
  captured artifact MUST carry honest, independently measured baselines.
  `ParseBenchmark` rejects a non-positive `answer_time_ms`/`token_budget`; do not
  weaken that guard, and do not capture placeholder baselines to pass a run.

## Tests

- `score_test.go` — fixture passes, parse rejections, and one failure-mode test
  per criterion.
- `grounding_test.go` — builds real packets through `internal/query` emitters and
  asserts the fixture budgets are honest.
