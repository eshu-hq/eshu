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
| (a) drains | `fact_work_items` residual rows and `shared_projection_intents` nonterminal rows both reach their snapshot bound. The `shared_projection_intents` check is the decisive one — a zero `fact_work_items` queue alone misses held projection intents (see #3859). The `repo_dependency` domain subset is reported because it is the primary drain signal. |
| (b) graph truth | Required correlations exist (`rc-1` deployable-unit, `rc-3` cross-repo `DEPENDS_ON`, ...). Per-label node and per-relationship edge counts are reported against the snapshot tolerances. |
| (c) query truth | Canonical HTTP responses (`GET /api/v0/repositories`, `GET /api/v0/status/operator-control-plane`) carry their required shape. |
| (d) timing | The pipeline wall time stays within a budget multiple. |

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

The first landing runs a **minimal corpus** (5 repos) and blocks only on the
drains, the existence of `rc-1`/`rc-3`, the two HTTP query shapes, and the timing
budget. The 20-repo node/edge count tolerances and the cassette-dependent
correlations (`rc-2` `RUNS_IN`, `rc-4` `RUNS_IMAGE`) are reported as **advisory**
(`WARN`) so latent gaps surface without blocking. Promoting them to required —
together with the full 20-repo corpus — is intentional follow-up work, gated on
each being green.

The blocking correlation set is configurable
(`golden-corpus-gate -required-correlations=rc-1,rc-3`) so the gate can be
widened one assertion at a time as the underlying behaviour is proven.

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
