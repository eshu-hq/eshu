<!-- docs-catalog
title: Debug A Failing Ifá Gate
description: Per-gate triage for the Ifá CI gates, with the exact local execution needed to reproduce each one.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Debug a failing Ifá gate

Each Ifá gate proves a different failure mode. Start with the failing `RUN` or
`TEST` command. Copy the matching unescaped `local.command` or
`local.test_command` from the CI gate registry at `specs/ci-gates.v1.yaml`.
Use the generated [CI gates reference](../reference/ci-gates.md) to confirm the
primary-command and self-test order, not as a copy-and-paste source: Markdown
table cells escape literal `|` characters used by some test-name regular
expressions. This keeps a local red on the reported path rather than silently
running a different or incomplete command.

## `ifa-contract-layer`

```bash
cd go && go test ./internal/ifa ./internal/ifa/materializededges ./cmd/ifa -count=1
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
(`bash scripts/verify-ifa-fault-injection.sh`) drives 49 cells — a
fault-free baseline, a killed worker, a forced lease expiry, one failed graph
write, a mid-drain backend restart, a duplicate delivery, a generation-2 delta
retract, and per-family killed-worker and failed-graph-write cells for
sql_relationships, code_calls, documentation_edges and rationale_edges, plus a
scoped baseline and two recovery cells each for deployable_unit_edges,
codeowners_ownership_edges, repository-dependency edges, submodule_pin_edges,
inheritance_edges, shell_exec, workload_dependency,
kubernetes_namespace_environment, and iam_instance_profile_role, and a shared baseline
plus three failed-graph-write and three runner-lease killed-worker recovery
cells for the handles_route/runs_in/invokes_cloud_action trio — with zero
durable dead letters throughout.

In CI the 48 cells other than the shared fault-free baseline are
split across 4 shards that run in
parallel, and the fault-free baseline is repeated in every shard rather than
partitioned into one — every recovery cell compares its graph against a
baseline captured on the same runner, so CI executes 52 cell runs
for a 49-cell matrix. Locally the command above runs all of them in one
pass. `--shard k/4` runs one shard, and `--list-cells` prints the partition
without starting anything.

### Reading a red check, and reproducing it

CI runs this gate as **four parallel shards**, so a failure shows up as
`fault-injection (shard N/4)` rather than a single `fault-injection` check. The
shard number in the check name is the one you need:

List the cells a shard owns, reproduce that shard alone, or print the whole
matrix:

```bash
bash scripts/verify-ifa-fault-injection.sh --list-cells --shard 3/4
bash scripts/verify-ifa-fault-injection.sh --shard 3/4
bash scripts/verify-ifa-fault-injection.sh --list-cells
```

Both `--list-cells` forms are hermetic: no Docker, no compose, no build. Run
them freely while something else holds the gate's ports.

Note that `cell_baseline` runs in *every* shard — it writes the canonical
digest each of the other cells in that shard compares against, so it is
repeated per runner rather than plumbed between them. Both `k` and `n` are
required; a bare `--shard 2` is rejected as malformed.

The baseline cell establishes the canonical digest, and the recovery cells
assert recovery to that *identical* graph. The delta-retract
cell is the exception and asserts something
different on purpose: generation 2 changes the graph, so its proof is the
exact expected edge set rather than digest equality. If you are tempted to
"fix" that cell by comparing it to the baseline, read its comment first — the
self-test rejects that edit.

Every cell also asserts the fault actually fired (a claimed-row wait, a
retry-count check, a restart sentinel, or a redelivered-row count), so a
script that silently never triggers cannot report a false pass. If a cell's
assertion names "the scripted fault never fired," the bug is in the fault
wiring, not in recovery itself — start there before suspecting the reducer.

A ninth cell, the SQL-anchored failed graph write, runs alongside the rest. It
was held out for months because its non-vacuity check reported that the fault
never fired in CI. The fault did fire; the assertion that read the fired-fault
marker matched with `rg`, which is not installed on the fault-injection runner,
so "command not found" was indistinguishable from "the marker does not name
this operation". If a fault cell ever reports an inert fault again, confirm the
checker itself can run before believing the verdict (#5974).

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

## `ifa-replay-drive`

```bash
bash scripts/test-verify-ifa-replay-drive.sh
```

This is the hermetic structural mirror for the Docker-backed
`verify-ifa-replay-drive.sh`, which drives the demo-org GCP cassette through
`eshu-ifa drive` and proves the drive enqueued real work before draining. The
mirror pins the parts of that contract that could drift silently: strict
mode, a unique Compose project on non-default ports, the drive-then-drain
ordering, and the exact drain SQL. A red mirror means the verifier's
structure changed out from under one of those pins — read the failing
assertion, then diff `scripts/verify-ifa-replay-drive.sh` against what the
mirror expects.

## General triage order

1. Identify the failing `RUN` or `TEST` entry in the generated
   [CI gates reference](../reference/ci-gates.md), then copy the corresponding
   unescaped `local.command` or `local.test_command` from
   `specs/ci-gates.v1.yaml` before touching anything. When the runner reported
   `TEST`, run `local.test_command`; running only the primary command cannot
   reproduce that failure.
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
