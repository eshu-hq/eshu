# SQL relationship retract evidence (#5116)

## Runtime and production shape

Proof used NornicDB `timothyswt/nornicdb-cpu-bge:v1.1.11` at immutable digest
`sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`
through the Neo4j Go driver managed-transaction path used by `EdgeWriter`.
Async writes, embeddings, BM25, vector search, and search-index persistence were
disabled, and every run started from an empty ephemeral store.

The old path submitted these six existing label-specific statements through one
`ExecuteGroup` managed transaction:

| Source label | Relationship |
| --- | --- |
| `Function` | `QUERIES_TABLE` |
| `SqlView` | `REFERENCES_TABLE` |
| `SqlFunction` | `REFERENCES_TABLE` |
| `SqlTable` | `HAS_COLUMN` |
| `SqlTrigger` | `TRIGGERS` |
| `SqlTrigger` | `EXECUTES` |

NornicDB returned success but left every intended edge. The corrected path keeps
the query text, parameters, labels, and property anchors byte-for-byte equivalent
and executes the fixed six statements as separate auto-commit transactions.

## Correctness evidence

The production-mode regression is
`TestReducerSQLRelationshipRetractGraphTruth`. It creates every typed edge
through `EdgeWriter.WriteEdges`, retracts through `EdgeWriter.RetractEdges`, and
checks both repository and delta-file scopes.

| Assertion | Managed group | Sequential auto-commit |
| --- | ---: | ---: |
| Intended SQL edges remaining | 6/6 | 0/6 |
| Out-of-scope edge surviving | 1/1 | 1/1 |
| Wrong-evidence edge surviving | not deleted | 1/1 |
| Endpoint nodes surviving | 10/10 | all test nodes |
| Retract returned an error | no | no |

Before the change, both live subtests failed with
`retract: queries-table: count = 1, want 0`. After the change, both scopes pass,
all six intended edge counts are zero, scope/evidence controls remain one, every
node remains one, and a repeated retract succeeds.

The exact backend gate is:

```bash
ESHU_REPLAY_TIER_HTTP_PORT=22574 \
ESHU_REPLAY_TIER_BOLT_PORT=22687 \
bash scripts/verify-replay-tier.sh
```

It completed in 7 seconds with the SQL regression and the full replay tier green.

## Performance Evidence:

The theory shim used the same six statements and Neo4j Go driver
`Session.ExecuteWrite`/`tx.Run`/`Consume` loop as the production live executor.
On the same clean v1.1.11 store and SQL fixture:

| Execution mode | Result | Wall time |
| --- | --- | ---: |
| One managed transaction | wrong, 6/6 stale | 8.659 ms |
| Six auto-commit transactions | exact, 0/6 stale | 4.101 ms |

This is a correctness shim, not a repo-scale throughput claim. The after shape
has a fixed cardinality of six statements per SQL retract. It introduces no
unlabeled or all-graph scan: `Function` remains anchored by indexed `repo_id` or
`path`, while the small SQL entity labels retain their existing label-specific
anchors. `UNION` was rejected because v1.1.11 returned only its first branch in
the assessment probe; the unlabeled fallback was rejected because its apparent
speed depended on an unsafe broad scan. The stop threshold was any stale
in-scope edge, any deleted control/node, or an obvious unbounded scan; the
sequential shape crossed none of them.

## No-Observability-Change:

No metric, span, log field, worker, lease, queue, or status contract changes.
Each statement still passes through the existing executor and
`WrapRetryableNeo4jError`; a partial failure returns immediately and follows the
existing shared-projection retry/dead-letter path. The statements are scoped and
idempotent, so replaying the whole six-statement retract after a partial failure
is safe. Operators retain the existing reducer shared-projection duration,
failure, retry, and terminal-status signals.
