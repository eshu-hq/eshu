# Scaled And Remote Performance Runs

## Resource-Qualified Claims

An absolute wall-clock target is valid only for its named reference profile;
it is not a hardware-independent product guarantee. Before using a run to
accept or reject an absolute target, record a non-sensitive measured resource
envelope: CPU architecture and logical CPU count, physical memory bytes,
storage kind, operating-system class, and any container CPU or memory limits.
Set `absolute_target_applicable` explicitly in the manifest.

Also record a human-readable `machine_profile`: category, provider or vendor,
model, cloud instance type when applicable, and a display name such as
`AWS EC2 <instance type>, 128 GiB` or `MacBook Pro, 32 GiB`. This makes evidence
understandable to contributors, while the measured resource envelope remains
the authority for comparability.

The resource envelope must be comparable to the accepted reference profile for
`absolute_target_applicable` to be true. A free-form `hardware_class` label is
useful for routing but is not sufficient evidence by itself. Differences in
CPU generation, throttling, storage latency, memory pressure, virtualization,
or container limits can invalidate an absolute-duration comparison even when
the nominal memory size matches.

Contributor runs on smaller or different machines remain useful. They may
prove correctness and a same-machine relative before/after improvement when
both runs use the same resource envelope and workload. Classify those results
as non-comparable to the reference profile and do not report a missed absolute
target as an Eshu regression. Do not publish a minimum hardware recommendation
from a single machine; establish it from repeated measurements across named
resource classes.

Machine capacity is only the supply side. Capture the Compose process demand as
well: configured replicas, CPU limits, memory limits/reservations, and a
phase-tagged time series for every service. Summarize peak CPU, peak working-set
memory, memory percentage, block I/O, restart count, and OOM state per service,
plus host memory pressure, load, swap, and I/O wait. A final `docker stats`
snapshot is not sufficient; it can miss the peak and cannot attribute pressure
to a pipeline phase.

Configured service limits are inputs and must match for a wall-clock comparison.
Observed service usage is an outcome: report its before/after delta, but do not
require it to be identical because reducing resource demand may be the intended
win. Missing usage evidence makes capacity/efficiency conclusions incomplete,
even when otherwise comparable wall-clock evidence remains usable with a caveat.

## Remote Preflight

Before reading recently merged skills, evidence, or code, refresh Git truth:

```bash
git fetch origin
git merge-base --is-ancestor <merge-commit> origin/main
```

Read from refreshed `origin/main`, the merge commit, or the dedicated worktree.
Do not treat a stale local main checkout as merged truth.

Before a scaled or full-corpus run, capture and compare:

- local and remote Eshu commit, stable patch ID when rebased, and clean state;
- graph backend commit, image digest or immutable image ID;
- Compose files, service topology, and owner process count;
- corpus identity and repository count;
- clean-volume versus preserved-volume state;
- schema/bootstrap state;
- effective worker, queue, timeout, connection-pool, and memory knobs;
- pprof and resource sampling state;
- the measured resource envelope and whether the reference-profile target is
  applicable;
- configured Compose service limits and phase-tagged per-service resource
  sampling;
- controller terminal condition and expected minimum runtime.

Bind evidence to the exact source input hash, built binary or image digest,
proof corpus, and harness or browser-runner hash. A stale image or runner labeled
as the final proof is a correctness defect in the evidence, not a bookkeeping
detail.

Remote source must come from Git: push the reviewed feature branch, then fetch
and check out or fast-forward it on the remote machine. Do not use `rsync` or
copy an unreviewed worktree. Keep hosts, users, IPs, key paths, and remote
checkout paths in user-local configuration, never in this repository.

Stop the run before launch if the intended topology or profile differs from the
baseline without an explicit experimental reason.

## Milestones And Terminal Truth

Capture these as separate timestamps and elapsed seconds where applicable:

- launch;
- schema/bootstrap ready;
- collection complete;
- source-local projection complete;
- queue terminal;
- shared materialization complete;
- vector/search readiness complete;
- post-drain finalizers complete;
- bootstrap process exit;
- API ready;
- MCP ready;
- first representative query success.

Never compare queue terminal with process exit or query readiness as if they
were the same metric. The primary terminal event must be identical between old
and new runs.

Report every duration as exact seconds plus a human-readable value, for example
`1205.924s (20m05.924s)`.

## Run Manifest

Every scaled or remote run used as evidence must produce a machine-readable run
manifest following [run-manifest.md](run-manifest.md).
The manifest records identity, topology, workload, milestones, truth counts,
resource evidence, readback, cleanup, and caveats.

The detailed manifest is operator-local and must not be committed. If the PR
needs a committed public aggregate, render it through
`specs/scale-benchmark-artifact.v1.yaml` and validate it with
`scripts/verify-scale-benchmark-artifact.sh`; do not invent a second public
artifact schema.

Public evidence may include only a run basename. Never publish a hostname, IP,
user, private key path, workstation path, remote checkout path, credential, or
secret-bearing DSN.

## Baseline Promotion

Keep one named accepted manifest for each proof profile in operator-local state.
Promote a replacement only when:

- the tested source is clean and its exact/equivalent commit is recorded;
- the primary metric boundaries and topology are explicit;
- the queue is non-empty, fully succeeded, and has zero failed/dead-letter work;
- required scope/readiness truth is terminal; and
- API, MCP, index status, and representative reads pass.

An accepted baseline may honestly classify the performance target as missed; it
is the current truthful comparison point, not a claim of success. Retain the
prior entry until promotion succeeds. Artifact-backed terminal counts override
earlier controller summaries when post-drain work reopens the queue.

## Comparability Gate

Before calculating a speedup, verify that old and new agree on:

- primary start and terminal events;
- corpus identity and cardinality;
- Eshu behavior profile and backend build;
- topology and service ownership;
- worker and connection knobs;
- clean or warm storage state;
- hardware class and the measured resource envelope;
- configured Compose replicas and resource limits;
- absolute-target applicability; and
- correctness/terminal counts.

If any load-bearing field differs, label the total non-comparable. Compare only
the matching phase or rerun the baseline. Never hide setup time inside one side
of a comparison.

## Stop Conditions

- Stop and profile when a healthy run regresses by more than 10% or 60 seconds.
- Stop a remote run at the declared time box unless it is making bounded,
  observable progress toward an operator-scale terminal proof.
- Do not launch with a time box shorter than the measured inherent cold-start
  floor; state the expected duration before launch.
- Do not merge a local-path win as an end-to-end target win when the target was
  missed.
- Run the full corpus at most once per proven candidate unless a documented
  comparability or proof failure requires a rerun.

## Retention Modes

Declare one mode in the run manifest before closeout:

- `stop-and-preserve`: stop readers, workers, controllers, and containers while
  retaining labeled data for likely review follow-up.
- `keep-live`: retain an interactive stack only when the user explicitly asks.
- `destroy`: remove the proof's labeled containers, volumes, networks,
  controllers, and temporary credentials after merge or final disposition.

Use a unique issue/run Compose project label and act only on that label. Never
use broad Docker pruning. For expensive proofs, `stop-and-preserve` avoids a
needless rerun while review is active; promotion or preservation does not waive
eventual cleanup.

## Evidence Carry-Forward After Rebase

An expensive remote result may carry forward across a base-only rebase only
when the old and new commits have identical stable patch IDs and the incoming
base diff does not touch the measured runtime, schema, topology, fixtures, or
proof harness. Record both commits and the patch ID. This does not waive
`make pre-pr`, review/attestation under `eshu-code-review`, or targeted local
proof on the rebased head.
