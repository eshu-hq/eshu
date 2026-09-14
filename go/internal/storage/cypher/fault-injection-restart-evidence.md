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

## Performance and observability

No-Regression Evidence (#6162): baseline
`48e77c61ecb6df06c7dcd4cbce3d37cb19ece5f5` and implementation commit
`c4b7485522a32e2858a9cb539cee6747e54404dc` both select
`fault_executor_off.go` in default builds. The recorder and restart sentinel
path are absent, so this change adds no production graph call, work item,
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

No-Observability-Change (production): baseline and after add no production
metric, span, structured log, status field, or dashboard series. The opt-in
tagged harness adds the stderr trigger line with group ordinal, statement
count, and capture duration. Failure-only artifacts retain the trigger JSON,
canonical graph dump and SHA-256/count manifest, work-item and GCP-fact
snapshots, reducer/projector/Compose logs, expected and actual backend
revisions, safe container and mount evidence, and the diagnostics completeness
manifest. These are CI fault-gate diagnostics, not production telemetry.
