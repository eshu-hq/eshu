# packetdogfood

`packetdogfood` scores the **investigation evidence packet dogfood benchmark**
(issue #3143): does Eshu's portable `investigation_evidence_packet.v2` artifact
produce a faster and more trustworthy first useful answer than raw repository
search or an existing Eshu tool drilldown?

It is a pure, deterministic scorer. It reads a captured benchmark artifact and
returns a pass/fail `Verdict`; it performs no graph, content, provider, or
network reads. The `eshu evidence-packet-dogfood` CLI command wraps it.

## Benchmark artifact

```jsonc
{
  "schema": "evidence_packet_dogfood.v1",
  "run_kind": "fixture",          // or "real_repo"
  "run_id": "...",
  "tasks": [
    {
      "name": "supply_chain_impact_blast_radius",
      "family": "supply_chain_impact",
      "approaches": [
        { "approach": "raw_files",       "answer_time_ms": 72000, "found_answer": false, "missing_evidence_named": false, "token_budget": 8200 },
        { "approach": "eshu_tools",      "answer_time_ms": 3800,  "found_answer": true,  "missing_evidence_named": false, "token_budget": 2400 },
        { "approach": "evidence_packet", "answer_time_ms": 1200,  "found_answer": true,  "missing_evidence_named": true,  "token_budget": 700 }
      ]
    }
  ]
}
```

Each task measures the three approaches — `raw_files` (manual file/grep reading),
`eshu_tools` (existing route/tool drilldown), and `evidence_packet` (one v2
packet) — on the four dogfood dimensions.

## Scoring

The benchmark passes only when the `evidence_packet` approach:

1. **family_coverage** — covers supply-chain impact, deployable drift, and
   service context.
2. **answer_correctness** — found the correct answer on every task.
3. **answer_time** — reached the first answer at least as fast as the best
   baseline on every task.
4. **token_efficiency** — stayed within the best baseline's token budget on
   every task.
5. **missing_evidence_clarity** — named missing evidence on every task, including
   a gap on at least one task that every baseline missed.

`ParseBenchmark` rejects an unknown schema, an empty task set, or a task missing
the `evidence_packet` approach or a baseline.

## Honesty

The checked-in fixture benchmark (`testdata/fixture_benchmark.json`) is grounded:
`grounding_test.go` builds a real packet for each implemented family through the
production emitters and asserts the fixture's claimed `evidence_packet` token
budgets are at least the real packet cost, so the benchmark numbers are never
understated relative to what the emitters produce.

The fixture run is reproducible in CI; the real-repository run is captured by an
operator (see the public reference doc) and scored with the same gate.
