# #5767 NornicDB relationship snapshot retry evidence

## Accuracy theory

Eshu's pinned NornicDB revision can expose an edge committed by another
transaction to `GetEdgesBetween` while the older transaction snapshot used by
`UpdateEdge` cannot load that edge. The stale writer returns the typed error:

```text
Neo4jError: Neo.ClientError.Statement.SyntaxError
(UNWIND MERGE chain relationship update failed: not found)
```

A fresh transaction sees the committed edge and can safely replay the same
MERGE. The Eshu retry classifier therefore accepts only that exact typed error
shape and only a MERGE statement or an all-MERGE group. Non-MERGE statements,
mixed groups, other error codes, and near-miss messages remain terminal.

The implementation does not reduce workers or batch sizes, add a readiness
gate, or change transaction boundaries. Exhausting the existing bounded retry
budget keeps the error queue-retryable under the established `write_conflict`
reason.

## Accuracy and concurrency proof

The regression suite was written first and failed because the observed error
escaped without a retry. After the classifier change:

```text
GOCACHE=$PWD/../.gocache go test ./internal/storage/cypher \
  -run 'RelationshipSnapshot' -count=1
ok github.com/eshu-hq/eshu/go/internal/storage/cypher

GOCACHE=$PWD/../.gocache go test -race ./internal/storage/cypher \
  -run 'RelationshipSnapshot' -count=1
ok github.com/eshu-hq/eshu/go/internal/storage/cypher
```

The deterministic two-writer test proves one stale writer retries, both
contributors complete, and the retry loop performs three total calls: one
winner, one conflict, and one replay. The negative matrix proves that unsafe
statement shapes do not replay.

Against the repository-pinned NornicDB image, the live test pins a stale
explicit transaction, commits the same `BUILT_FROM` edge from a winning
transaction, captures the exact backend error, and replays through the real
`RetryingExecutor`:

```text
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_URI=bolt://localhost:27689 \
ESHU_NORNICDB_RETRY_CONTRACT_LIVE=1 \
go test ./internal/storage/cypher \
  -run '^TestLiveNornicDBRelationshipSnapshotConflictRetryContract$' \
  -count=1 -v
--- PASS: TestLiveNornicDBRelationshipSnapshotConflictRetryContract
```

The final readback contains exactly one synthetic `BUILT_FROM` edge with the
retried scope.

## Performance proof

The benchmark exercises the unchanged success path for a one-statement,
500-row MERGE group. Ten two-second samples before and after the classifier
change produced:

| Revision | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Before | 21.76 | 64 | 1 |
| After | 22.29 | 64 | 1 |

`benchstat` reports `+2.46%` (`+0.53 ns/op`, `p=0.001`, `n=10`). This is below
the declared 10% stop threshold, preserves allocation behavior, and occurs in
a no-op harness even though the new classifier is reached only after an error.
The clean-volume golden-corpus wall-time and dead-letter results are recorded
below before promotion.

## Golden-corpus result

Two independent clean-volume runs used the mandatory live gate:

```text
docker compose -f docker-compose.yaml down -v
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
bash scripts/verify-golden-corpus-gate.sh
```

Both runs reported 507 pass, 0 required-fail, 0 dead letters, and a 98-second
pipeline wall time. Each first drain took 64 seconds against the 75-second
baseline and 86.2-second ceiling. Both maintenance-drain phases took 11 seconds,
one second over their advisory ceiling; that phase is outside the changed retry
path and did not affect the required gate.

## Observability evidence

No-Regression Evidence: successful writes take the same control flow, while the
new retry uses the existing bounded backoff and retry budget.

Observability Evidence: the existing structured retry log and
`eshu_dp_neo4j_deadlock_retries_total{write_phase,reason="write_conflict"}`
counter expose the event without placing raw errors, identifiers, or statements
in metric labels. Retry exhaustion remains visible to the existing projector
queue and dead-letter telemetry.
