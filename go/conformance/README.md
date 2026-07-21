# conformance — contributor conformance suite

The out-of-tree onboarding surface for the Eshu deterministic replay framework
([#4112](https://github.com/eshu-hq/eshu/issues/4112) / R-10, epic
[#4102](https://github.com/eshu-hq/eshu/issues/4102) §8). It lets a contributor
prove their collector extracts the right facts **with zero provider credentials
and zero Docker**, using the *same* assertion logic as the in-repo B-7
golden-corpus gate — just a smaller corpus and an offline cassette replay
instead of a live pipeline.

A green `go test ./conformance` run is the credential-free deterministic proof
that [#4047](https://github.com/eshu-hq/eshu/issues/4047) (the monorepo split)
points to for the **collector extraction** readiness criterion.

## What it does

```
starter cassette ─(replay, offline)→ facts ─(Observe)→ graph observation
                                                              │
        starter spec YAML ─(LoadSpec → goldengate.Snapshot)──┤
                                                              ▼
                          goldengate.Evaluate*  ── shared with the in-repo gate
                                                              ▼
                                                   pass / fail Report
```

- **Replay** uses `internal/replay/cassette.Source` — the same credential-free
  replay primitive the in-repo replay tiers use.
- **Observe** (`observe.go`) maps the starter `starter.*` fact kinds to
  `Repository` / `Directory` / `File` / `Package` nodes, `CONTAINS` edges, and a
  `DEPENDS_ON` edge that carries `evidence_kinds` and a `source_tool` property —
  so the suite exercises the evidence-narrowed correlation and edge-property
  qualifier path, not just bare triple counts. This is the one piece you replace
  for your own collector.
- **Evaluate** (`conformance.go`) feeds the in-memory observation into
  `internal/goldengate.Evaluate*` — the identical functions
  `cmd/golden-corpus-gate` runs against a live graph. **No forked assertion
  logic.**

## The 5-command flow

Run from a fresh clone. Every command is credential-free and Docker-free except
the optional re-record in step 4 (which needs only your collector's own live
credentials, never Postgres or Docker).

```bash
# 1. Clone and enter the Go module.
git clone https://github.com/eshu-hq/eshu && cd eshu/go

# 2. Run the starter conformance suite — proves your toolchain reproduces the
#    deterministic proof out of the box.
go test ./conformance -count=1

# 3. Describe what YOUR collector must project: edit the spec's node/edge counts,
#    required correlations, required-node property floors, and self-loop bounds.
$EDITOR conformance/testdata/starter-spec.yaml

# 4. Record your own tape once, against your real API, with your own credentials
#    (no Postgres, no Docker — the recorder commits nothing durable):
go run ./cmd/collector-<your-collector> -mode=record \
  -cassette-file=conformance/testdata/starter-cassette.json

# 5. Re-run credential-free. Green = your collector's extraction is conformant
#    and reproducible.
go test ./conformance -count=1
```

If you change the starter fact kinds, also update the `Observe` mapping in
`observe.go` so the new facts project to the labels/edges your spec asserts.

## Files

| File | Purpose |
|------|---------|
| `conformance.go` | `Run`, `replayFacts`, `LoadSpec`, and `Evaluate` (the shared-assertion driver). |
| `observe.go` | `Observe` — the collector-specific fact → graph-observation seam. |
| `conformance_test.go` | `TestConformance` (the headline) plus observation, regression-bites, evidence/edge-property, malformed-input, schema-validity, and determinism tests. |
| `testdata/starter-cassette.json` | The starter tape: a `hello-eshu` repo with two directories, three files, and one package dependency. Schema-valid against the R-3 cassette JSON Schema. |
| `testdata/starter-spec.yaml` | The starter spec: the contributor-facing twin of the B-12 golden snapshot, parsed into `goldengate.Snapshot`. |

## Why offline / what it does not cover

The suite proves **graph projection truth** — that a collector's facts project
the nodes, edges, and correlations the spec demands, deterministically. It does
**not** exercise the live reducer queue drain or a real graph backend; those need
the full pipeline and are covered by the in-repo `golden-corpus-gate` and the
`internal/replay/offlinetier` real-NornicDB tier. The split is deliberate: this
suite is the part a contributor can run anywhere, instantly, with nothing
installed but Go.

The `Observe` mapping is the one piece that is **not** shared: it is your
contributor-owned model of how your collector's facts project to nodes and
edges, so you must keep it in lockstep with your real projector. The assertion
logic (`goldengate.Evaluate*`) and the replay primitive are shared and never
forked; only the fact → observation mapping is yours to maintain. The in-repo
`golden-corpus-gate` and `offlinetier` tiers cross-check the real backend so a
drift between your `Observe` model and real projection is caught upstream.

## Running

```bash
cd go && go test ./conformance -count=1
```
