# Golden Corpus Gate (B-7)

The golden end-to-end corpus gate is the headline guard against "Eshu stops
working end to end." One command runs the full pipeline — `sync → discover →
parse → collect → reduce → query` — over a fixed repo corpus with every
credentialed collector replayed from cassettes, then diffs the live result
against a committed golden snapshot.

It exists because unit tests pass while the assembled pipeline silently breaks:
a queue that never drains, a correlation edge that stops being written, a query
shape that changes. The gate asserts the whole assembly, not the parts.

## What it proves

The gate asserts the four B-7 acceptance buckets:

| Bucket | Assertion |
| --- | --- |
| (a) drains | `fact_work_items` residual rows and `shared_projection_intents` nonterminal rows both reach their snapshot bound. The `shared_projection_intents` check is the decisive one — a zero `fact_work_items` queue alone misses held projection intents (see #3859). To avoid passing on an *unreduced* pipeline, the drain is **populated-then-drained**: it is accepted only after the reducer has been observed to emit the `repo_dependency` domain (`-require-populated-domains`), so a poll that fires before the reducer starts cannot read an empty `0/0` and pass. The `repo_dependency` subset is reported because it is the primary drain signal. |
| (b) graph truth | Required correlations exist (`rc-1` deployable-unit, `rc-3` cross-repo `DEPENDS_ON`, ...). Per-label node and per-relationship edge counts are reported against the snapshot tolerances. |
| (c) query truth | Canonical HTTP responses (`GET /api/v0/repositories`, `GET /api/v0/status/operator-control-plane`) carry their required shape. |
| (d) timing | The total pipeline wall time stays within a budget multiple, and — when the orchestrator supplies per-phase timings (B-11, #3804) — each gated phase stays within its `e2e-baseline.json` baseline. See [Macro per-phase regression (B-11)](#macro-per-phase-regression-b-11). |

## Moving parts

- **B-10 cassettes** (`testdata/cassettes/<collector>/supply-chain-demo.json`)
  replay every credentialed collector with no cloud credentials.
- **B-12 snapshot** (`testdata/golden/e2e-20repo-snapshot.json`) is the contract
  the live run is diffed against. Update it only under review when graph or query
  behaviour changes intentionally — never to paper over drift.
- **`golden-corpus-gate`** (`go/cmd/golden-corpus-gate`) is the typed,
  unit-tested assertion binary.
- **`scripts/verify-golden-corpus-gate.sh`** is the orchestrator that brings up
  Postgres + the graph backend, runs `bootstrap-index`, replays the cassettes,
  drains the projector and reducer, starts `eshu-api`, and invokes the gate.

## Minimal-then-grow

The first landing ran a **minimal corpus** (5 repos) and blocked only on the
drains, the existence of `rc-1`/`rc-3`, the two HTTP query shapes, and the timing
budget. The corpus has since grown one assertion at a time as each correlation
was proven green end to end: `rc-2` (`RUNS_IN`, the code→runtime bridge) and
`rc-4` (`RUNS_IMAGE`, the live workload→OCI manifest edge) are now **required**
alongside `rc-1` (deployable-unit) and `rc-3` (cross-repo `DEPENDS_ON`). The
20-repo node/edge count tolerances remain **advisory** (`WARN`) so latent gaps
surface without blocking.

The blocking correlation set is configurable
(`golden-corpus-gate -required-correlations=rc-3,rc-1,rc-2,rc-4`) so the gate can
be widened one assertion at a time as the underlying behaviour is proven.

## Macro per-phase regression (B-11)

B-2 (#3795) catches a per-*function* `ns/op` regression with benchstat; B-11
(#3804) catches a per-*phase* wall-clock regression that no single benchmark
would surface. The orchestrator captures the wall-clock of each pipeline phase
(`bootstrap`, `collect`, `first_drain`, `maintenance_drains`, `graph_query`),
emits it as `phase-timings.json`, and the gate compares each phase against the
committed baseline `testdata/golden/e2e-baseline.json`.

A gated phase passes when

```
observed <= baseline * (1 + regression_band)   OR   observed <= baseline + absolute_slack_seconds
```

The dual rule mirrors the reducer claim-latency contract's "1.10x OR +60s": the
relative band catches real regressions on the larger phases, while the absolute
slack absorbs integer-second timing jitter on the small phases.

- **`collect`** is recorded but **not gated** — it is dominated by the fixed
  collector settle window, not pipeline work.
- **On shared CI runners** the check is **advisory** (`-phase-regression-advisory`):
  GitHub's hosted runners vary run-to-run by more than the band, so a per-PR
  regression is reported as a `WARN` without a false red — the same reasoning
  behind the 2x total-wall-time budget multiplier.
- **On a controlled host** (consistent hardware) set
  `GATE_PHASE_REGRESSION_ADVISORY=false` to make the gated phases blocking; the
  committed baseline is valid there.

Recapture the baseline on the enforcement host after an intentional perf change:

```bash
bash scripts/refresh-e2e-baseline.sh   # runs the gate, folds observed seconds in
```

It updates only `baseline_seconds`; `gated` flags, notes, band, slack, and the
policy blocks are preserved. Review the diff and commit with a before/after
explanation, the same review bar as the B-12 snapshot.

## Running it

Static and unit checks (no Docker):

```bash
cd go && go test ./cmd/golden-corpus-gate -count=1
bash scripts/test-verify-golden-corpus-gate.sh
```

Full live run (needs Docker):

```bash
bash scripts/verify-golden-corpus-gate.sh
# --no-compose  assume Postgres + graph are already up
# --keep        retain services + work dir for debugging a failure
```

In CI the gate runs as the **Golden Corpus Gate** workflow, required on any PR
that touches a pipeline phase (collector, parser, projector, reducer, query,
storage, the pipeline command binaries, the cassettes, or the snapshot).
