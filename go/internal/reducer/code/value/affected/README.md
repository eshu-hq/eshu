# internal/reducer/code/value/affected

## Purpose

Answers the value-flow refresh emit gate (issue #6785): which repos own at
least one Function calling a cloud action, constrained by producer-written
keys (repo ids, workload ids, or principal uids). A producer run whose rows
touch no cloud-calling repo can never grow a cloud sink, so its ACK emits no
completion event.

## Ownership boundary

**Owns:** the three gate statements and their row parsing
(`ReposWithCloudCallers*`).

**Does not own:** the producer handlers that extract the keys, the ACK SQL
that reads the reported count, or the fixpoint solver itself (`code/value`).
