<!-- docs-catalog
title: How Eshu Proves Itself
description: Explains the priority order and proof systems that back every Eshu change — accuracy, then performance, then concurrency.
type: concept
audience: practitioner, maintainer
entrypoint: true
landing: false
-->

# How Eshu proves itself

Eshu's own rule for itself is blunt: a wrong graph is a product failure, a slow
correct answer is fixable, and a correct fast answer that breaks under
concurrent load is not done. That order — accuracy, then performance, then
concurrency — is the repo's life motto, and it is not a slogan. Every proof
system described here exists to check one of those three, in that order, before
a change ships.

Most engineering teams have a test suite and hope it is enough. Eshu has
several proof systems, each owning a different failure mode, because "the
tests passed" does not tell you whether a graph write survived two workers
racing for the same node, or whether the queue actually drains after you kill
a process mid-handler.

## The proof landscape

Four systems cover the three-part motto:

- **Ifá** is the conformance platform: one scenario (an *Odù*) replayed through
  contract checks, a worker-count matrix, load and saturation, and injected
  faults. It is the newest system and the subject of the rest of this testing
  story — see [The Ifá conformance platform](ifa-conformance-platform.md).
- **The golden-corpus gate (B-7/B-12)** proves projected truth. A cassette is
  a recorded or synthetically generated stream of `facts.Envelope` values,
  replayed through the same `cassette.Source` seam a live collector would
  feed — no network call, no credential, byte-identical on every replay. The
  golden corpus is a fixed 20-repository set of these cassettes plus a
  committed snapshot (`testdata/golden/e2e-20repo-snapshot.json`) naming the
  node/edge counts and required correlations that corpus must produce; the
  B-7 gate drives the cassettes through the real pipeline and asserts the
  projected graph, the HTTP API, the MCP surface, and the pinned playbook
  answers all agree with that snapshot. This is Eshu's oldest and broadest
  end-to-end proof.
- **perfcontract** binds every published performance number in the docs to the
  code that measures it, so a doc claim ("queue claim latency stays under
  50&nbsp;ms p95") cannot drift silently from what the system actually does.
  Each threshold carries an enforcement class: `hermetic_gate` runs in every
  CI run; `operator_gated` needs a controlled environment and consistent
  hardware.
- **Telemetry coverage and security lanes** prove the operational and supply-
  chain side: every new metric, span, or pipeline stage is checked against a
  documented contract, and gosec/govulncheck/nancy/Trivy cover the dependency
  and image surface.

These are not four unrelated test suites bolted together. They share
machinery on purpose: Ifá reuses the golden-corpus snapshot to derive its
expectations, and both reuse the same cassette codec and canonicalizer that
record/replay testing has used for years. Nothing here is a parallel universe
of test doubles — it is the same production seams, exercised harder.

## Accuracy, performance, concurrency — in that order

A change that makes Eshu faster but wrong does not ship. A change that is
correct and slow is a bug to fix, not a reason to skip the correctness proof.
A change that is correct and fast under one worker but corrupts a node under
four workers is the failure this whole apparatus exists to catch before a user
does.

Concretely, this shows up as sequencing: fix and prove accuracy first (does the
graph hold the right nodes, edges, and evidence), then measure performance
against a documented threshold, then run the concurrency matrix. Serialization
— dropping worker counts, batching writes down to size 1 — is never an
acceptable substitute for fixing a race. The determinism-under-load layer
described below exists specifically to catch that shortcut.

## One door: `make prove` and `make pre-pr`

A contributor should not have to remember which of a dozen verification
scripts applies to their change. Two commands are the door:

- `make pre-pr` selects and runs the credential-free CI gates your changed
  paths require — formatting, lint, build, the targeted race lane, and every
  exactness gate the registry maps to your diff.
- `make prove` is Ifá's own credential-free mirror: the contract-layer test,
  the hermetic structural mirrors for the Docker-backed determinism matrices,
  and a coverage reconcile, plus the real Docker matrix when Docker is present
  and changed paths select it.

Both commands are driven by the same gate registry
(`specs/ci-gates.v1.yaml`), so "which gate covers my change" has one source of
truth instead of tribal knowledge. See
[Run the proof suite](../guides/run-the-proof-suite.md) for the walkthrough.

## Where this leaves CI

CI stays authoritative. `make pre-pr` and `make prove` exist so a
credential-free failure is never first discovered twenty minutes into a CI
run — but CI re-checks everything, including the Docker-backed matrices a
laptop without Docker cannot run locally. A green local run is a strong
signal, not a substitute for a green CI run.

## Next

- [The Ifá conformance platform](ifa-conformance-platform.md) — what an Odù
  is, the four layers, and the honest limits of what runs in CI.
- [Run the proof suite](../guides/run-the-proof-suite.md) — the commands, in
  order, with real output.
- [CI gates reference](../reference/ci-gates.md) — every gate, generated from
  the registry.
