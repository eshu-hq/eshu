<!-- docs-catalog
title: Debug A Failing Ifá Gate
description: Per-gate triage for the five Ifá CI gates, with the exact local command to reproduce each one.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Debug a failing Ifá gate

Each Ifá gate proves a different failure mode. Start with the exact local
command from `specs/ci-gates.v1.yaml` — it is the same command CI runs, so a
local red is the real red, not a CI-only artifact.

## `ifa-contract-layer`

```bash
cd go && go test ./internal/ifa ./cmd/ifa -count=1
```

A failure here means one of: a payload no longer validates against its
fixturepack schema, a typed round-trip lost or reshaped a field, or a Odù's
graph evidence no longer resolves the correlation the coverage manifest binds
it to. Run with `-run` scoped to the failing test name and `-v` for detail;
`coverage_falsegreen_test.go`'s error messages name the missing evidence kind
directly, for example `relationship DEPLOYS_FROM missing evidence kind(s)
[KUSTOMIZE_RESOURCE_REFERENCE]`.

## `ifa-determinism`

```bash
bash scripts/test-verify-ifa-determinism.sh
```

This is the hermetic structural mirror — it checks the real gate script's
shape (strict mode, isolated Compose ports, the worker-count loop) without
Docker. A failure here is almost always a script edit, not a real
determinism defect. To reproduce the real determinism defect this gate
guards against, run the live matrix with Docker:

```bash
bash scripts/verify-ifa-determinism.sh --keep
```

`--keep` retains the work directory so a digest mismatch leaves you the full
canonical-graph diff instead of a tmpdir that vanished on cleanup. Never
retry this to green and never lower the worker count — a genuine divergence
is the concurrency defect the gate exists to catch, and both of those moves
would hide it instead of fixing it.

## `ifa-dead-letter-matrix`

```bash
bash scripts/test-verify-ifa-dead-letter-matrix.sh
```

Same hermetic-mirror-versus-live-gate split as `ifa-determinism`. The live
gate mutates a fact kind's `schema_version` to an unsupported major, drives
it at worker counts 1, 2, and 4, and asserts the durable `fact_work_items`
dead-letter set is identical across every run. If it fails, compare the
`DeadLetterRecord` fields the report prints — `work_item_id`, `stage`,
`domain`, and `failure_class` all have to match, not just the count.

## `ifa-fault-injection`

```bash
bash scripts/test-verify-ifa-fault-injection.sh
```

The hermetic mirror. The live gate
(`bash scripts/verify-ifa-fault-injection.sh`) drives five cells — a
fault-free baseline, a killed worker, a forced lease expiry, one failed graph
write, and a mid-drain backend restart — and asserts each recovers to the
identical canonical graph with zero durable dead letters. Every cell also
asserts the fault actually fired (a claimed-row wait, a retry-count check, or
a restart sentinel), so a script that silently never triggers cannot report a
false pass. If a cell's assertion names "the scripted fault never fired,"
the bug is in the fault wiring, not in recovery itself — start there before
suspecting the reducer.

## `ifa-load-saturation`

```bash
cd go && go test ./internal/ifa/saturation/ ./internal/ifa/throughput/ -race -count=1
```

This gate is fully hermetic — no Docker step. A saturation failure means the
backpressure gate did not bound in-flight writes the way the test expects: a
dead-letter count above zero, or a residual queue after the pressure round
releases. A throughput failure means the committed scope or fact totals
differ across worker counts, which means the concurrent driver dropped or
double-counted work.

## General triage order

1. Reproduce with the exact `local.command` from
   [CI gates reference](../reference/ci-gates.md) before touching anything —
   confirm the failure is real, not a stale build artifact.
2. If it is a hermetic mirror, check whether the real Docker-backed gate
   still fails the same way. A mirror failure that the real gate does not
   reproduce is a mirror bug, not a platform bug.
3. Read the failure message before hypothesizing. Every Ifá gate is written
   to name the specific mismatch (a missing evidence kind, a digest that
   differs, a fault that never fired) rather than a bare non-zero exit.
4. Never retry to green and never lower the worker count. Both hide the
   defect this platform exists to surface.

See [Run the proof suite](run-the-proof-suite.md) for the full `make prove` /
`make pre-pr` walkthrough, and
[The Ifá conformance platform](../concepts/ifa-conformance-platform.md) for
what each layer is actually proving.
