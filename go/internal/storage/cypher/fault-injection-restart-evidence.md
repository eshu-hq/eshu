# Fault-injection restart evidence

The `ifafaultinjection` build records the exact statement group whose executor
call returned success immediately before
`restart-backend-between-phase-groups` exposes its sentinel. The JSON record
sits beside the sentinel as `<sentinel>.trigger.json` and includes the group
ordinal, executor surface, operations, Cypher, parameters, and drain metadata.
It is written through a temporary file and renamed before the sentinel is
created, so the restart watcher cannot observe the fault without a complete
trigger record.

This is a test-only diagnostic contract. Default builds do not contain the
recorder, and the CI collector must only upload it for the repository's
committed synthetic fixtures. The record identifies one acknowledged executor
request at the restart boundary. It does not prove backend durability,
reconstruct every submitted group, or distinguish projection, scheduling, and
backend causes by itself.

The fault gate derives the expected backend revision from rendered Compose. For
this test-only fault gate, a `NORNICDB_IMAGE` override must also set
`IFA_FAULT_EXPECTED_NORNICDB_REVISION`; diagnostics retain the expected and
running revisions and remain incomplete when they differ.

On a CI failure, the workflow's `--keep` mode retains the normal
`graph-restartbackend.dump` when the post-drain capture was reached. If the
drain gate fails before that capture, cleanup performs one bounded canonical
graph read into `graph-restartbackend-failure.dump` before Compose teardown. It
never performs that additional read when the normal dump already exists. The
failure manifest hashes the retained bytes and marks the diagnostic set
incomplete if the graph read times out or fails. A default local run without
`--keep` emits the existing stderr diagnostics and removes its temporary work
directory instead of collecting this retained artifact set.

## Hosted diagnosis

The first hosted run with this capture path, PR #6651 run 34605794613, retained
a complete failure artifact from `cell_restartbackend`. The baseline and failure
dumps both contained 678 nodes, while the failure dump lacked exactly one
63-edge synthetic GCP scope. Its durable work row recorded the previously
unseen sibling of the #6142 restart conflict:

```text
Neo.ClientError.Statement.SyntaxError
UNWIND MERGE chain relationship create failed:
end node nornic:0cb9ed9e-3825-4f75-b278-649f7fada124 does not exist
```

The relationship item dead-lettered at attempt 1 as `projection_bug`. The
typed retry guard covered only NornicDB's adjacent `start node` branch. It now
accepts either exact endpoint role with a non-empty id under the existing
MERGE-shaped single-statement or all-statements-replay-safe group gate;
malformed queries and broader missing-node errors remain terminal.
The run used the Compose-resolved revision
`3722b483c02c38a8e046d198f8768f200f31023c`. It is not evidence for the
separately deployed NornicDB v1.3.1 environment.

## Performance and observability

No-Regression Evidence (diagnostics, #6162): baseline
`48e77c61ecb6df06c7dcd4cbce3d37cb19ece5f5` and implementation commit
`c4b7485522a32e2858a9cb539cee6747e54404dc` both select
`fault_executor_off.go` in default builds. The recorder and restart sentinel
path are absent, so the capture path adds no production graph call, work item,
queue row, or request-path work. No runtime timing was measured or is claimed.
The hermetic tagged regression uses a recording executor with two completed
groups; the trigger group contains one canonical-upsert statement and one
prepared row. It proves the complete atomic JSON record exists before the
sentinel and is not overwritten. The shell fixture proves failure capture
performs exactly one bounded graph read when the regular restart dump is
absent, no extra read when it exists, preserves the original exit status,
rejects timeout values outside 1--30 seconds before collection, and fails
completeness on missing artifacts or backend-revision mismatch. These local
fixtures use neither Postgres nor a live graph backend, so backend version and
terminal queue counts are not applicable locally. Before merge, the hosted
shard must provide its existing drain, dead-letter, and digest result against
the Compose-resolved NornicDB revision; this note does not claim that result.

No-Regression Evidence (classifier): successful graph writes do not enter the
error classifier. The new branch performs no graph call and adds no success-path
work. On the exact end-node failure it uses the existing `write_conflict` loop,
bounded by `RetryingExecutor.MaxRetries`, instead of returning a terminal error
after the first attempt. Focused regressions prove one failure followed by
success makes exactly two calls through both executor APIs and records the
existing `write_conflict` reason. The fail-closed cases prove unrelated errors,
non-MERGE single statements, and non-replay-safe groups make one call. No
runtime timing is claimed.

Observability Evidence: no metric, span, structured-log field, status field, or
dashboard series is added. The classified end-node shape now emits the existing
`neo4j transient error, retrying` log and
`eshu_dp_neo4j_deadlock_retries_total{reason="write_conflict"}` counter instead
of a `projection_bug` dead letter. The opt-in tagged harness adds the stderr
trigger line and retains the trigger JSON, canonical graph dump and manifest,
work-item and GCP-fact snapshots, runtime logs, backend provenance, and the
diagnostics completeness manifest. These are CI fault-gate diagnostics, not
production telemetry.
