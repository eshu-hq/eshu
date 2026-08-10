# Fair Kubernetes Runtime Probe (#5834)

## Contract

Supply-chain finding list and explain reads collect at most 200 distinct,
non-empty subject digests. Digests are sorted before work begins. For `D`
digests, each receives `floor(200/D)` candidate slots and the lexical first
`200 mod D` digests receive one additional slot. The quotas are deterministic,
sum to 200, and are never zero.

Each digest is queried separately with the existing CALL-wrapped three-label
`RUNS_IMAGE` statement. A fixed pool runs at most 32 graph reads concurrently;
this is not a serial fallback. Each `GraphQuery.Run` opens its own Neo4jReader
session through the concurrent-safe driver pool. Workers write only their
digest-indexed result slot. Fan-in flattens those slots in digest order and
makes one Postgres current-authorization call.

Scoped callers query exactly the quota and always receive
`workload_refs_truncated: null`. All-scopes callers query `quota + 1`, so raw
graph work is at most 400 candidates. After authorization, each digest is
trimmed to its quota and the probe retains at most 200 distinct digest-scoped
workload references; findings that share a digest repeat the same bounded refs.
All-scopes truncation is `true` only when current authorized refs
exceed the quota, `false` only when the raw graph read exhausted within the
quota, and `null` otherwise.

## Concurrency and failure model

- Conflict domain: one immutable input digest and its private result slot.
- Shared state: the bounded job channel, first-error cancellation, wait group,
  and atomic observed-concurrency counters. Candidate slices are not shared
  between workers.
- Graph transaction boundary: one read-only autocommit Bolt session per digest
  attempt. Sessions are never shared.
- Postgres transaction boundary: one read after graph fan-in validates the
  workload owner winner and RUNS_IMAGE edge generation independently.
- Retry boundary: Neo4jReader may retry one digest read through its existing
  bounded availability policy. An exhausted error cancels siblings; all workers
  drain or exit and the handler returns only after the wait group completes.
- Idempotency: every operation is read-only. A retry cannot duplicate graph or
  Postgres state.
- Partial failure: no runtime refs or probe metadata are attached until every
  graph read and the Postgres authorization read succeed.

The graph reads are separate autocommit snapshots, not one cross-digest graph
snapshot. A concurrent projection can therefore change between digest reads.
The Postgres gate rejects candidates whose owner or edge generation is no
longer current, but it cannot make the earlier Bolt reads one atomic snapshot.
Positive evidence remains bound to an exact graph candidate plus current owner
and edge ledger rows; this multi-snapshot window is the remaining risk and is
visible through the child-span counts and request error telemetry.

## Performance Evidence:

The theory probe used the default Compose NornicDB source revision
`3722b483c02c38a8e046d198f8768f200f31023c`, image ID
`sha256:1afd1f92af1de69bfd336e6b1d4d9136019309c0640ace9b54e1cccba1e4d8d5`,
with embeddings, BM25, vector search, async writes, and Heimdall disabled. The
same graph contained 200 digests, one workload per digest, and 1,000 workloads
on the lexical first digest. Each row below used three warmups and 15 measured
reads of the same input shape.

| Shape | Digests | Rows | Digests represented | p50 | p95 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Prior global limit | 1 | 200 | 1 | 1.296 ms | 2.187 ms |
| Balanced, concurrency 16 | 1 | 200 | 1 | 1.294 ms | 2.730 ms |
| Prior global limit | 10 | 200 | 1 | 1.575 ms | 2.545 ms |
| Balanced, concurrency 16 | 10 | 29 | 10 | 2.035 ms | 3.655 ms |
| Prior global limit | 100 | 200 | 1 | 1.758 ms | 3.782 ms |
| Balanced, concurrency 16 | 100 | 101 | 100 | 13.391 ms | 18.609 ms |
| Prior global limit | 200 | 200 | 1 | 1.602 ms | 2.021 ms |
| Balanced, concurrency 32 | 200 | 200 | 200 | 6.954 ms | 8.424 ms |

The 200-digest balanced p95 adds 6.403 ms while preserving evidence for every
digest. A concurrency sweep measured 16 workers at 16.079 ms p95, 24 at
9.332 ms, 32 at 8.424 ms, 48 at 36.045 ms, and 64 at 251.970 ms. The fixed
ceiling is therefore 32; higher fanout saturates the backend or connection pool.

Public-safe disposable theory-harness command, captured exit code:

```bash
NORNICDB_IMAGE=eshu-nornicdb-pr290:3722b483c02c go run .
# exit 0
```

Focused implementation proof commands record their exit codes directly:

```bash
cd go
go test ./internal/query \
  -run 'KubernetesRuntime|SameKubernetesRuntimeEvidence|OpenAPI.*RuntimeContext' \
  -count=1
# exit 0

go test -race ./internal/query \
  -run 'KubernetesRuntime|SameKubernetesRuntimeEvidence' -count=1
# exit 0
```

## Observability Evidence:

The `supply_chain.kubernetes_runtime_probe` child span reports
`eshu.subject_digest_count`, `eshu.kubernetes_runtime_query_count`,
`eshu.kubernetes_runtime_concurrency_limit`,
`eshu.kubernetes_runtime_max_concurrency`,
`eshu.kubernetes_runtime_candidate_limit`, `eshu.graph_candidate_count`,
`eshu.authorized_current_workload_count`,
`eshu.runtime_confirmed_digest_count`, `eshu.runtime_workload_count`,
`eshu.kubernetes_runtime_truncated_digest_count`, and
`eshu.kubernetes_runtime_unknown_digest_count`. Planned work is recorded before
fanout and result counts initialize to zero, so an operator can distinguish a
planned probe failure from an empty, permission-hidden, or truncated result.
