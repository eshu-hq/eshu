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

The committed integration harnesses bind the shipped Cypher, the concrete
Postgres authorization store, and the real concurrent Neo4jReader. The tested
code commit is `1568a2a7b56b9a224b80e61ba41144b1999ac196`; the harness rejects a
production Cypher hash other than
`1900bbe55bc2f6e63da43ec9f8a6c1ed67e416702ab718aebd4db6ea2c794fee`.
It ran against NornicDB source revision
`3722b483c02c38a8e046d198f8768f200f31023c`, image ID
`sha256:1afd1f92af1de69bfd336e6b1d4d9136019309c0640ace9b54e1cccba1e4d8d5`,
and PostgreSQL 18.4.

The #6011 base rebase changed the Kubernetes cassette and B-12 snapshot, not
the measured query, Postgres gate, or live harness. Blob comparison between the
tested commit and the rebased branch returned identical object IDs:

| File | Git blob |
| --- | --- |
| `supply_chain_impact_kubernetes_runtime_probe.go` | `bce5ca484e284893b8fb3cc234fdf0d2a3e7d297` |
| `supply_chain_impact_kubernetes_runtime_probe_fair.go` | `fc548f44218bc4d3cd31bcb63638f6a05b021bc5` |
| `kubernetes_runtime_workload_store.go` | `1c81d97983cf93a7810756af20973410a69e78d3` |
| `supply_chain_impact_kubernetes_runtime_probe_performance_live_test.go` | `bca0979ee7bc38bf2e710eaf9fa3aa54ca49f87c` |
| `kubernetes_runtime_workload_store_fairness_live_test.go` | `c741b71d2ef765057f054adf342cd0fb5dd995b7` |

The post-rebase golden run below then exercised the new cassette digest through
the shipped API and MCP handlers.

The graph held 200 digests: 1,000 workloads for the lexical first digest and
two for every other digest. Each latency row used three warmups and 15 measured
requests. Concurrent measurements used four requests, two warmups, and eight
measured rounds on the same graph and Postgres state.

| Request shape | Prior global limit p50 / p95 | Balanced p50 / p95 | Truth result |
| --- | ---: | ---: | --- |
| One request | 2.178 / 3.434 ms | 7.925 / 9.196 ms | prior: 200 refs from 1 digest; balanced: 200 refs from 200 digests |
| Four concurrent requests | 4.709 / 12.659 ms | 30.207 / 40.158 ms | every measured request retained its lane's exact truth result |

The exact recorded run stayed below 41 ms at p95. An immediately preceding
same-shape run on the same host observed transient tails of 262.799 ms for one
balanced request and 314.557 ms for four concurrent requests; those are still
below the capability's 1,000 ms local-full-stack p95 budget, but they show why
the evidence does not claim a tighter SLO. The fixed driver pool and worker
ceiling were both 32. The final run observed a maximum of 32 graph reads, 400
all-scope candidates, 11,861 successful one-attempt Neo4jReader spans across
the measurement set, and a successful request immediately after forced parent
cancellation. The paired Postgres plans measured 1.464 ms for the scoped
200-candidate shape and 3.388 ms for the all-scope 400-candidate shape,
excluding planning time.

The live test initially failed because a fixture created nodes, a relationship,
and relationship properties in one NornicDB statement; the relationship existed
but its properties were nil. The accepted fixture now uses the production
writer's `MATCH`/`MATCH`/`MERGE`/`SET` shape and asserts the persisted source,
mode, digest, scope, and generation before timing the read. This keeps a fixture
quirk from becoming a false product failure.

Public-safe disposable command shape, captured exit code:

```bash
graph_container="eshu-5834-nornic-proof"
postgres_container="eshu-5834-postgres-proof"
trap 'docker stop "$graph_container" "$postgres_container" >/dev/null 2>&1 || true' EXIT
docker run -d --rm --name "$graph_container" \
  -e NORNICDB_NO_AUTH=true -e NORNICDB_BOLT_PORT=7687 \
  -p 127.0.0.1::7687 eshu-nornicdb-pr290:3722b483c02c
docker run -d --rm --name "$postgres_container" \
  -e POSTGRES_PASSWORD=eshu-proof -p 127.0.0.1::5432 postgres:18.4
graph_port="$(docker port "$graph_container" 7687/tcp | awk -F: 'NR == 1 {print $NF}')"
postgres_port="$(docker port "$postgres_container" 5432/tcp | awk -F: 'NR == 1 {print $NF}')"
for attempt in {1..90}; do
  if nc -z 127.0.0.1 "$graph_port" >/dev/null 2>&1 && \
     docker exec "$postgres_container" pg_isready -U postgres -d postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
cd go
ESHU_KUBERNETES_RUNTIME_PROBE_PERFORMANCE_LIVE=1 \
ESHU_NEO4J_URI="bolt://127.0.0.1:${graph_port}" \
ESHU_KUBERNETES_RUNTIME_PROBE_POSTGRES_DSN="postgresql://postgres:eshu-proof@127.0.0.1:${postgres_port}/postgres?sslmode=disable" \
ESHU_KUBERNETES_RUNTIME_PROBE_POSTGRES_DISPOSABLE=1 \
go test -tags=integration ./internal/query \
  -run '^(TestLiveKubernetesRuntimeProbePerformance|TestKubernetesRuntimeWorkloadGatePreserves(DigestFairness|SingleDigestSentinel)Live)$' \
  -count=1 -v
# exit 0
```

Focused implementation proof commands also capture their exit codes directly:

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

## Golden-corpus response proof

The golden-corpus gate (B-7) indexes the checked-in fixture repositories and
checks graph, HTTP, and MCP answers against the saved B-12 snapshot. After
#6011 updated the Kubernetes fixture to the full 64-hex digest, the snapshot
pins one exact finding on both list surfaces and the singular explanation on
both explain surfaces. Each response must carry these three current workloads:

- `kubernetes_live:supply-chain-demo:/v1/pods:default:supply-chain-demo-pod`
- `kubernetes_live:supply-chain-demo:apps/v1/deployments:default:supply-chain-demo`
- `kubernetes_live:supply-chain-demo:apps/v1/replicasets:default:supply-chain-demo-7f8d9`

The same assertions require `candidate_limit: 200`,
`workload_refs_truncated: false`, and the existing `runtime_confirmed` value
for both deployment truth and version resolution. The list shapes set both
their minimum and maximum result count to one, so wildcard assertions cannot
be satisfied by different findings. A hostile watcher empties the workload
refs or removes the probe block and requires the evaluator to fail.

The saved corpus has cloud runtime evidence for this digest too. Version
resolution keeps `cloud_runtime_probe` as the winner when both sources match,
and the public response does not serialize the winner's evidence-kind string.
The corpus proof therefore binds the Kubernetes source through its exact refs
and probe metadata, without adding a new wire field or tier label.

```bash
cd go
go test ./internal/goldengate/... ./cmd/golden-corpus-gate/ -count=1
# exit 0

cd ..
bash scripts/test-verify-golden-corpus-gate.sh
# exit 0

cd go
go test ./internal/ifa/... ./cmd/ifa -count=1
# exit 0

cd ..
bash scripts/test-verify-ifa-determinism.sh
# exit 0
bash scripts/test-verify-ifa-dead-letter-matrix.sh
# exit 0

cd go
go run ./cmd/ifa coverage \
  -specs-dir ../specs \
  -snapshot ../testdata/golden/e2e-20repo-snapshot.json
# exit 0: 24 pass, 0 required-fail, 173 advisory-warn

cd ..
bash scripts/verify-golden-corpus-gate.sh
# exit 0: 548 pass, 0 required-fail, 3 advisory timing warnings;
# pipeline elapsed 213s, required ceiling 1800s
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
