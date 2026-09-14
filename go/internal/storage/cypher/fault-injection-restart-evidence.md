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
