# Evidence Packet Dogfood Benchmark

The evidence-packet dogfood benchmark (issue #3143) measures whether Eshu's
portable [investigation evidence packets](../investigation-evidence-packet.md)
produce a **faster and more trustworthy first useful answer** than raw repository
search or an existing Eshu tool drilldown.

It is scored by `eshu evidence-packet-dogfood`, which reads a captured benchmark
artifact and exits non-zero when the packet approach loses on any dimension. The
scorer lives in `go/internal/packetdogfood`.

## What it compares

For each investigation task, three approaches are measured:

| Approach | What it is |
| --- | --- |
| `raw_files` | Reading raw repository files / grep output by hand. |
| `eshu_tools` | An existing Eshu route or tool drilldown (typically several calls). |
| `evidence_packet` | One portable `investigation_evidence_packet.v2` artifact. |

Each approach is measured on four dimensions: **answer time**, **correctness**,
**missing-evidence clarity** (does it name the gaps?), and **token budget**.

The benchmark defines tasks for **supply-chain impact**, **deployable drift**, and
**service context**, the three families the packet emitters target.

## Pass criteria

The packet approach must, across the benchmark:

1. **family_coverage** — cover supply-chain impact, deployable drift, and service
   context.
2. **answer_correctness** — find the correct answer on every task.
3. **answer_time** — reach the first answer at least as fast as the best baseline
   on every task.
4. **token_efficiency** — stay within the best baseline's token budget on every
   task.
5. **missing_evidence_clarity** — name missing evidence on every task, including a
   gap on at least one task that every baseline missed.

## Fixture run (checked in, reproducible)

`go/internal/packetdogfood/testdata/fixture_benchmark.json` is the reproducible
fixture run. Its `evidence_packet` token budgets are **grounded**: a test builds a
real packet for each implemented family through the production emitters and
asserts the fixture's claimed budgets are at least the real packet cost (the real
fixture packets measure ~668–803 tokens), so the numbers are never understated.

| Task | Family | raw_files | eshu_tools | **evidence_packet** |
| --- | --- | --- | --- | --- |
| supply_chain_impact_blast_radius | supply_chain_impact | 72.0s / 8200 tok / gaps:no | 3.8s / 2400 tok / gaps:no | **1.2s / 700 tok / gaps:yes** |
| deployable_unit_truth | deployable_unit | 51.0s / 6100 tok / gaps:no | 3.2s / 2100 tok / gaps:no | **1.1s / 800 tok / gaps:yes** |
| runtime_drift_reconciliation | drift | 64.0s / 7400 tok / gaps:no | 4.1s / 2600 tok / gaps:no | **1.3s / 850 tok / gaps:yes** |
| service_context_dossier | service_context | 58.0s / 6800 tok / gaps:no | 4.5s / 2500 tok / gaps:no | **1.4s / 900 tok / gaps:yes** |

Result: the packet is the fastest approach, uses ~3× fewer tokens than the next
best (`eshu_tools`) and ~9× fewer than `raw_files`, and is the only approach that
names the missing hops explicitly. `eshu evidence-packet-dogfood` scores this run
**PASSED**.

Reproduce:

```bash
eshu evidence-packet-dogfood \
  --from go/internal/packetdogfood/testdata/fixture_benchmark.json
```

## Real-repository run (operator-captured)

The proof gate requires at least one fixture scenario (above) **and** one real
indexed Eshu repository scenario. Capture the real run against a live API:

1. Index a real repository and bring up a local API (see
   [Local Testing](../local-testing.md)).
2. For each family, time and size the three approaches:
   - `raw_files`: time a manual grep/read that answers the same question.
   - `eshu_tools`: time the existing route/tool drilldown.
   - `evidence_packet`: `eshu investigation export --family <f> --subject ... --format json`,
     timing the single call and sizing the artifact.
3. Record each measurement as an `ApproachResult` in a benchmark artifact with
   `"run_kind": "real_repo"`.
4. Score it: `eshu evidence-packet-dogfood --from real-run.json`.

The real-repository artifact is captured by an operator because it depends on a
live indexed repository; the scoring gate is identical to the fixture run, so the
same pass criteria apply.
