# NornicDB v1.3.1 alignment evidence (#6162)

## Decision

Eshu's current Compose, Helm, R-5 replay, and Kubernetes governance-proof
defaults use one immutable upstream artifact:

```text
timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962
```

The OCI index contains the following Linux manifests:

| Platform | Manifest digest | Validation in this change |
| --- | --- | --- |
| `linux/amd64` | `sha256:c0b5f73c55bd56a6764d1833665252b98a30b332248f0233f5f4eab4dc0d2ca1` | Docker runtime and live Eshu proof |
| `linux/arm64` | `sha256:d787abe61d92c67761bdbd21ae5224aad904f2a13283d46d13fdfb6269b86b7b` | Manifest and verifier branch only; no arm64 runtime claim |

The amd64 container reported `NornicDB v1.3.1`. The published image has no
OCI source-revision label, so the immutable index digest, selected platform
manifest, platform, and binary-reported version form its provenance contract.

## Why the prior default was rejected

The prior Compose source build, `eshu-nornicdb-pr290:3722b483c02c`, passed a
fault-free baseline but corrupted the graph after a backend restart while the
queue still reached a terminal state. On the eight-project Ifá fixture, the
baseline had 678 nodes, 707 edges, and 627 GCP relationships with digest
`280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052`.
The restart result had 678 nodes, 833 edges, and 753 GCP relationships with
digest
`51e00ff9afffd104d403a8f94cb7f5b31ca2d3c1b30362a79e8e3fa2436fe0fe`;
126 GCP relationships joined endpoints from different projects. This was a
false-green convergence signal, not missing retry or dead-letter handling.

Replacing only the backend artifact with the v1.3.1 digest removed the
cross-project relationships and restored the baseline digest. The permanent
Ifá assertion now checks the exact fixture scope-to-project map, expected edge
counts, `reducer/gcp-relationships` producer, `CloudResource` endpoint labels,
and both endpoint `account_id` values before accepting either baseline or
restart output.

## Local proof on 2026-09-11

All commands ran from the feature worktree with `GOTOOLCHAIN=go1.26.6` and
`CC=clang` for Go commands that compile tree-sitter.

| Contract | Command | Result |
| --- | --- | --- |
| Image and package defaults | `docker compose config --quiet` and `helm lint deploy/helm/eshu` | pass |
| Runtime and Ifá contracts | `cd go && go test ./internal/ifa/graphdump ./cmd/ifa ./internal/runtime ./internal/query/codequery/relationships -count=1` | pass |
| Ifá hostile/static mirror | `bash scripts/test-verify-ifa-fault-injection.sh` | pass; 49 cells and four-shard exact cover |
| Kubernetes provenance verifier | `bash scripts/test-verify-k8s-two-team-governance-proof.sh` | pass when the runtime reports either the exact index or the architecture-matched amd64/arm64 child; wrong index, repository, platform child, version, and invented source revision fail closed |
| Remote-evidence gate selection | `bash scripts/test-verify-remote-validation-artifacts.sh` | 34 passed; runner, verifier, helper, test, and fixture paths select the gate |
| Relationship identity | `go test ./internal/storage/cypher -run 'TestProvenanceEdgeWriterLive(LegacyRowSetMigration\|SamePairAssertionIsolation)' -count=1 -v` against the exact v1.3.1 amd64 container | pass; legacy migration, duplicate delivery, eight-way concurrent delivery, retry, and scoped retract isolation |
| R-5 replay | `bash scripts/verify-replay-tier.sh` | pass; offline graph truth and tombstone/idempotent replay completed in 87 seconds; SQL UNION branches passed live in 48 seconds |
| B-7 golden corpus | `bash scripts/verify-golden-corpus-gate.sh` | 561 pass, 0 required failures, 1 advisory timing warning; 147 seconds total against the 1,800-second blocking ceiling |

The B-7 run staged 31 repositories, replayed 19 credentialed collector
sources and 37 scope generations, and finished every drain with zero residual,
dead-letter, required shared-intent, cross-scope-completion, or unroutable work.
Its graph, API, MCP, demo-answer, and container-image lifecycle assertions all
passed. One maintenance drain completed in 33 seconds against the 30-second
advisory target; the blocking pipeline wall-time and every correctness gate
passed. These results support compatibility and no blocking performance
regression, not a speedup or improved maintenance-latency claim. The advisory
target remains unchanged.

## Restart determinism

One full `--shard 4/4` Ifá run passed every assigned recovery cell. Its common
baseline completed in 24 seconds. The restart cell completed in 21 seconds,
fired the restart sentinel during the drain, reached zero dead letters and
terminal queue counts, checked 627 GCP relationships with `cross_scope=0`, and
matched the baseline digest:

```text
280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052
```

The supported sharder was then checked before a second run:

```text
$ bash scripts/verify-ifa-fault-injection.sh --list-cells --shard 4/21
cell_baseline
cell_restartbackend
```

That independent focused run again passed. After the final rebase, the same
slice was repeated: its baseline completed in 18 seconds and restart completed
in 20 seconds, fired the sentinel, reached zero
dead letters, checked the same 627 GCP relationships with `cross_scope=0`, and
produced the same digest in both cells. The same corpus, worker count, terminal
conditions, storage reset, and graph boundary were used in both runs.

## Storage and rollback boundary

This change does not claim that an existing graph volume can be upgraded in
place or downgraded safely. The supported migration stops graph writers,
preserves the old graph volume, starts v1.3.1 on a fresh graph volume, rebuilds
from the durable Postgres fact store, verifies queues and graph truth, and only
then cuts traffic over. Rollback restores the preserved old volume or rebuilds
another fresh graph. An older NornicDB binary must not open a volume modified
by v1.3.1 without separate reverse-compatibility proof.

## Operational signal

No Eshu runtime telemetry contract changes in this alignment. Operators retain
the existing queue residual/dead-letter metrics and logs, graph-truth gates,
container health checks, and NornicDB version output. The Kubernetes proof
records the configured index, raw Pod-runtime image ID, node architecture, and
binary-reported version. Its verifier accepts only the exact immutable index or
the architecture-matched published child.
