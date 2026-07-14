# Profiling And Concurrency

Use this page for worker-count diagnostics, `ESHU_PPROF_ADDR`, and phase CPU
capture.

## Concurrency Tuning Reference

Set any variable to `1` to force sequential processing during debugging. Do not
ship single-worker settings as a fix for a concurrency bug.

| Env var | Default | Service | Controls |
| --- | --- | --- | --- |
| `ESHU_PROJECTION_WORKERS` | `min(NumCPU, 8)` | Bootstrap Index | Concurrent bootstrap projection goroutines |
| `ESHU_DEFERRED_BACKFILL_CONCURRENCY` | `min(NumCPU, 8)`, hard cap `8` | Bootstrap Index / Ingester | Concurrent per-repository batch transactions in the deferred relationship-evidence backfill; set `1` at `ESHU_POSTGRES_MAX_OPEN_CONNS=1` |
| `ESHU_SNAPSHOT_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner: `NumCPU` | Ingester / Bootstrap | Concurrent repository snapshot goroutines |
| `ESHU_PARSE_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner: `NumCPU` | Ingester / Bootstrap | Concurrent file-parse workers inside a repository snapshot |
| `ESHU_PROJECTOR_WORKERS` | `min(NumCPU, 8)`; NornicDB local-authoritative: `NumCPU` | Ingester / Projector | Concurrent source-local projection workers |
| `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` | `NumCPU`, clamped to `16` | Bootstrap Index / Ingester / Projector | Parallel canonical `entities` and `entity_containment` phase grouped writes on NornicDB |
| `ESHU_REDUCER_WORKERS` | NornicDB: `NumCPU`; Neo4j: `min(NumCPU, 4)` | Reducer | Concurrent reducer intent execution goroutines |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | NornicDB: `workers`; Neo4j: `workers * 4` capped at `64` | Reducer | Reducer intents leased per claim cycle |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | unset / disabled | Reducer | Optional cap on cross-scope semantic entity materialization claims after source-local drain |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | Reducer | Maximum code-call shared intents scanned or loaded for one accepted repo/run before failing safely |
| `ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT` | `8` | Reducer | File-scoped CALLS projection partitions |
| `ESHU_CODE_CALL_PROJECTION_WORKERS` | `4` | Reducer | Concurrent file-scoped CALLS partition workers |
| `ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS` | `1` (`4` in remote E2E) | Reducer | Fixed source-repository acceptance-unit shards; allowed values are `1`, `2`, and `4` |
| `ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL` | `5m` | Reducer | Fail-closed shard quarantine and lease ownership window |
| `ESHU_REPO_DEPENDENCY_PROJECTION_CYCLE_TIMEOUT` | `45s` | Reducer | Deadline for the whole repository acceptance cycle |
| `ESHU_SHARED_PROJECTION_WORKERS` | `min(NumCPU,4)` | Reducer | Concurrent shared projection partition goroutines |
| `ESHU_SHARED_PROJECTION_PARTITION_COUNT` | `8` | Reducer | Partitions per shared projection domain |
| `ESHU_SHARED_PROJECTION_BATCH_LIMIT` | `100` | Reducer | Intents processed per partition batch |
| `ESHU_SHARED_PROJECTION_POLL_INTERVAL` | `500ms` | Reducer | Shared projection poll interval; idle cycles back off up to `5s` |
| `ESHU_SHARED_PROJECTION_LEASE_TTL` | `60s` | Reducer | Partition lease time-to-live |

Repo-dependency workers do not split one repository's edge rows across workers.
The acceptance-unit shard owns the repository's complete ordered
retract-then-rewrite cycle, so the same repository remains serialized while
unrelated repositories can overlap. Unsupported worker values fall back to `1`;
do not use `3` or raise the lane above the proven four-worker ceiling. Each
process adds hostname, PID, and a boot-unique nonce to its lease-owner prefix.
The reducer rejects unsafe timing at startup unless:

```text
repo-dependency lease TTL
  > whole-cycle timeout + canonical graph-write timeout + 30s
```

The `5m` default satisfies both the normal `30s` canonical-write timeout and
the remote-E2E `120s` timeout. A failed, canceled, or ambiguous cycle does not
release its shard early. The same owner waits out the lease TTL before retrying,
while independent shards continue to run.

When changing queue or worker behavior, also prove:

- expired claims can be reclaimed
- overdue claims surface through status
- ack failures emit logs and metrics
- structured logs keep failure class, queue name, and work item identity

## Scanner-Worker Resource Isolation

Scanner workers run as a hosted runtime for claim-driven security analyzer
work that is too CPU-heavy or memory-heavy for reducer drain. The default
runtime path emits explicit warning source facts until a concrete analyzer is
configured.

Before turning a concrete scanner-worker analyzer on by default, prove these
signals:

- workflow claim age, lease renewal, retry, and dead-letter behavior;
- analyzer wall time, CPU seconds, peak memory bytes, target count, and result
  count;
- fact output count and fact kind, with scanner workers emitting source facts
  only;
- pprof access on a private address for the worker process;
- no raw target locators in metric labels, retry payloads, or dead-letter
  payloads.

Use the resource envelope in
[Security Intelligence](../security-intelligence.md#resource-and-deployment-guidance)
as the starting point for Compose and Kubernetes tests. A scanner worker that
needs higher limits should get a separate pool or Deployment rather than
raising reducer memory.

## Process Profiling

Each Go runtime binary ships an opt-in `net/http/pprof` endpoint. It is
disabled by default and gated by `ESHU_PPROF_ADDR`.

```bash
ESHU_PPROF_ADDR=:6060 eshu-ingester

go tool pprof -seconds=30 http://127.0.0.1:6060/debug/pprof/profile
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
curl -sS http://127.0.0.1:6060/debug/pprof/goroutine?debug=2 > goroutines.txt
```

A bare port like `:6060` is rewritten to `127.0.0.1:6060` so a typo cannot
silently expose profiling endpoints on a routable interface. Supply an explicit
host only when you intend broader exposure; pprof reveals goroutine dumps, heap
snapshots, and CPU profiles and must be treated as credential-grade.

For Helm deployments, use workload-specific `env` maps when only one runtime
needs a profile:

```yaml
ingester:
  env:
    ESHU_PPROF_ADDR: "127.0.0.1:6060"
resolutionEngine:
  env:
    ESHU_PPROF_ADDR: "127.0.0.1:6061"
```

## Remote E2E Worker Profiles

For hosted remote E2E performance debugging, keep the base stack unchanged and
add the pprof overlay only for the run that needs profiles:

```bash
docker compose --env-file .env.remote-e2e \
  -f docker-compose.remote-e2e.yaml \
  -f docker-compose.remote-e2e.pprof.yaml \
  --profile seed up --build
```

When profiling Grafana, Prometheus/Mimir, Loki, or Tempo, include the matching
observability overlays:

```bash
docker compose --env-file .env.remote-e2e \
  -f docker-compose.remote-e2e.yaml \
  -f docker-compose.remote-e2e.observability.yaml \
  -f docker-compose.remote-e2e.pprof.yaml \
  -f docker-compose.remote-e2e.observability.pprof.yaml \
  --profile grafana --profile prometheus-mimir \
  --profile loki --profile tempo up --build
```

The overlay sets `ESHU_PPROF_ADDR=0.0.0.0:6060` inside each worker container,
then publishes that container port on the remote host loopback interface. That
keeps the profiler private to the test host while still allowing an operator to
use an SSH tunnel from their laptop.

| Service | Host endpoint |
| --- | --- |
| `bootstrap-index` | `127.0.0.1:19660` |
| `ingester` | `127.0.0.1:19661` |
| `resolution-engine` | `127.0.0.1:19662` |
| `workflow-coordinator` | `127.0.0.1:19663` |
| `collector-terraform-state` | `127.0.0.1:19664` |
| `collector-oci-registry` | `127.0.0.1:19665` |
| `collector-package-registry` | `127.0.0.1:19666` |
| `collector-aws-cloud` | `127.0.0.1:19667` |
| `collector-confluence` | `127.0.0.1:19668` |
| `projector` | `127.0.0.1:19669` |
| `collector-vulnerability-intelligence` | `127.0.0.1:19670` |
| `scanner-worker` | `127.0.0.1:19671` |
| `collector-sbom-attestation` | `127.0.0.1:19672` |
| `collector-security-alerts` | `127.0.0.1:19673` |
| `collector-pagerduty` | `127.0.0.1:19674` |
| `collector-jira` | `127.0.0.1:19675` |
| `collector-grafana` | `127.0.0.1:19676` |
| `collector-prometheus-mimir` | `127.0.0.1:19677` |
| `collector-loki` | `127.0.0.1:19678` |
| `collector-tempo` | `127.0.0.1:19679` |

Example captures from the remote host:

```bash
go tool pprof -seconds=30 http://127.0.0.1:19662/debug/pprof/profile
curl -sS http://127.0.0.1:19662/debug/pprof/goroutine?debug=2 \
  > resolution-engine-goroutines.txt
go tool pprof http://127.0.0.1:19667/debug/pprof/heap
```

Use this only after logs, metrics, and status identify the runtime that owns the
cost. Do not add the overlay to normal Compose, Helm, or Kubernetes defaults.

## CPU Capture During A Phase

For perf investigations that need a CPU profile from the ingester, or matched
profiles from both the ingester and a co-running NornicDB child process,
`scripts/capture-cpu-profile.sh` takes a run directory, the ingester pprof
endpoint, and an optional NornicDB pprof endpoint.

```bash
ESHU_PPROF_ADDR=127.0.0.1:0 \
NORNICDB_PPROF_ENABLED=true \
NORNICDB_PPROF_LISTEN=127.0.0.1:19091 \
eshu graph start --workspace-root /path/to/repo --logs terminal \
  > /tmp/run-X/run.log 2>&1 &

INGESTER_PPROF=$(rg -o '"pprof server listening","addr":"[^"]+","service_name":"ingester"' \
  /tmp/run-X/run.log | rg -o '127\.0\.0\.1:[0-9]+' | head -1)

PPROF_CPU_S=20 PPROF_SLEEP_S=5 \
  scripts/capture-cpu-profile.sh /tmp/run-X "$INGESTER_PPROF" 127.0.0.1:19091
```

For ingester-only parser profiling, omit the third argument or pass `-` and
trigger from the stage before the parse window:

```bash
PPROF_LOG_MARKER='"stage":"pre_scan"' PPROF_CPU_S=30 PPROF_SLEEP_S=0 \
  scripts/capture-cpu-profile.sh /tmp/run-X "$INGESTER_PPROF" -
```

Profiles land in `$RUN_DIR/profiles/`:

- `ingester-cpu-${PPROF_CPU_S}s.pb.gz`
- `nornicdb-cpu-${PPROF_CPU_S}s.pb.gz` when a NornicDB endpoint is provided
- `nornicdb-{heap,allocs}.pb.gz` plus `*goroutines.txt` snapshots when a
  NornicDB endpoint is provided
- `watcher.log`

If the CPU profile is zero bytes, check `watcher.log` for curl exit codes.
`rc=7` usually means the pprof endpoint went away before curl finished; shorten
`PPROF_CPU_S` or keep the stack alive longer.
