<!-- docs-catalog
title: Scale Slots And The Perf Contract
description: Documents the scale-lab corpus slots Ifá adopts, their fan-out, and the perfcontract enforcement split.
type: reference
audience: practitioner, maintainer
entrypoint: false
landing: false
-->

# Scale slots and the perf contract

Ifá's load and saturation layer does not invent a scale taxonomy. It adopts
`specs/scale-lab-corpus.v1.yaml` (issue #3170) and binds each corpus slot to
an amplification fan-out and a `perfcontract` enforcement class in
`go/internal/ifa/slots.go`. A lockstep test asserts every bound slot id is
still present in the spec, so this page cannot silently drift from the
source of truth.

This page documents the mechanism — which slot runs where, and how big the
fan-out is. It does not publish target latency or throughput numbers; issue
#4589 owns publishing those SLO targets.

## The five slots

| Slot id | Scale | Ifá scopes | Resources per scope | Enforcement |
| --- | --- | --- | --- | --- |
| `smoke/synthetic_contracts` | smoke | 0 (schema-only, not an amplification target) | — | `hermetic_gate` |
| `small/single_repo_multidomain` | small | 4 | 16 | `hermetic_gate` |
| `medium/representative_20_50` | medium | 24 | 32 | `operator_gated` |
| `large/full_corpus_release` | large | 64 | 64 | `operator_gated` |
| `pathological/fanout_correlation` | pathological | 48 | 128 | `operator_gated` |

Scope and resource counts are `go/internal/ifa/slots.go`'s chosen fan-out for
each slot's proof class, not the spec's own repository-count language (the
spec describes real-corpus repository counts; Ifá's amplifier works in
synthetic scopes). A slot is `Amplifiable()` — a legal `AmplifyAtSlot`
target — only when both scopes and resources are positive; the smoke slot is
schema-only by design and fails closed if you try to amplify it.

## Hermetic versus operator-gated, honestly

Only `smoke` and `small` are `hermetic_gate`: credential-free, no Docker, run
in every `make prove` and CI pass via `go/internal/ifa/throughput` and
`go/internal/ifa/saturation`. Both packages model the corpus and the
backpressure gate in memory — no Postgres, no graph backend, no network — so
they never flake on a busy CI runner.

`medium`, `large`, and `pathological` are `operator_gated`. They need
consistent hardware and a controlled environment to produce a meaningful
latency number, and they are **not exercised by hermetic CI today**. The
amplification mechanism (`AmplifyAtSlot`) works at every scale; what does not
yet exist is a calibrated, blocking gate running those slots at scale. This
is stated as an open item in the platform's own design doc, not glossed
over.

## The measurement contract

`specs/scale-lab-corpus.v1.yaml` also defines the metrics a slot run must
report against an accepted same-shape baseline: fact rows/sec, queue-claim
latency p95, reducer drain seconds, graph-write p95, API/MCP p95, retry
count, dead-letter count, memory high-water mark, and correlation-fanout
candidates p95. Ifá's throughput and saturation runners assert the subset of
this contract that is meaningful hermetically — committed totals and
backpressure/dead-letter shape — and defer the wall-clock latency numbers to
the operator-gated slots where hardware is consistent enough for them to
mean something.

## Threshold enforcement classes

`go/internal/perfcontract` defines the two enforcement classes Ifá reuses
rather than inventing a second perf contract:

- `EnforcementHermeticGate` — measured by a credential-free gate that already
  runs in CI.
- `EnforcementOperatorGated` — needs a controlled environment (real corpus,
  live backend, consistent hardware); the contract still keeps its documented
  number honest, but the measurement itself is not a hermetic CI gate.

See [The Ifá conformance platform](../concepts/ifa-conformance-platform.md)
for how the load and saturation layer fits next to the other three, and
[Debug a failing gate](../guides/debug-a-failing-gate.md#ifa-load-saturation)
for the `ifa-load-saturation` gate's local reproduction command.
