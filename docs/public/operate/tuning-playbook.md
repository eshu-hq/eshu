# Tuning Playbook

Use this page when Eshu is running but answers are stale, indexing is slow, or
graph writes are timing out. This is the operator path. Deep knob details live
in [NornicDB Tuning](../reference/nornicdb-tuning.md).

## First Rule

Classify the symptom before changing timeouts, worker counts, or batch sizes.

Do not lower worker counts or force single-threaded queue drains as a shipped
fix for concurrency or graph-write problems. Use that only as a measurement
lane while you prove the real bottleneck.

## Stale Answers

Start here when API and MCP are healthy but the answer looks old.

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/index-status
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  http://localhost:8080/api/v0/status/index
curl -fsS http://localhost:8080/admin/status
```

For Kubernetes:

```bash
kubectl -n eshu logs statefulset/eshu --tail=100
kubectl -n eshu logs deployment/eshu-resolution-engine --tail=100
```

Decision path:

| Signal | Meaning | Next step |
| --- | --- | --- |
| ingester is still processing | source facts are still being emitted | wait or inspect repository access and discovery logs |
| reducer pending or retrying work is non-zero | graph/read-model truth is not fully projected | inspect resolution-engine logs and queue age |
| failed or dead-letter work is non-zero | projection needs operator attention | inspect failure class before retrying |
| all queues are clean but result is wrong | likely query truth, parser, or correlation issue | capture the exact question, repo, entity, and evidence gap |

## Slow Indexing

First find out whether the cost is repository shape, content writes, graph
writes, or queue projection.

```bash
eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-advisory.json
```

Use the discovery report to decide whether `.eshuignore` or
`.eshu/discovery.json` should exclude generated, vendored, archived, or copied
third-party trees.

Do not raise global timeouts because one repository contains generated output.
Fix the repository scope first.

## Graph Write Timeout

Graph write timeouts usually need NornicDB evidence, not a blind timeout bump.

Collect:

- failing phase and label.
- row count.
- grouped statement count.
- timeout duration.
- queue state.
- NornicDB image or binary version.
- Eshu commit.

Then use [NornicDB Tuning](../reference/nornicdb-tuning.md). Raise
`ESHU_CANONICAL_WRITE_TIMEOUT` only for a focused correctness-validation lane
unless the evidence proves the hosted default needs to change.

## Queue Backlog

Check whether workers are busy, stuck, or retrying:

```bash
curl -fsS http://localhost:8080/admin/status
```

For Kubernetes:

```bash
kubectl -n eshu logs deployment/eshu-resolution-engine --tail=200
kubectl -n eshu logs statefulset/eshu --tail=200
```

Decision path:

| Signal | First response |
| --- | --- |
| queue depth falling | wait and keep watching oldest age |
| queue depth rising, no failures | check graph-write durations and Postgres connection pressure |
| repeated retry class | fix the owning reducer/query/storage path |
| dead letters | stop and inspect the exact failure before replay |

## High Memory

Check whether pressure is in Eshu, Postgres, NornicDB, or generated source
inputs.

For Compose:

```bash
docker compose ps
docker stats
```

For Kubernetes:

```bash
kubectl -n eshu top pods
kubectl -n eshu describe pod -l app.kubernetes.io/instance=eshu
```

If NornicDB is rebuilding search indexes after restart, confirm
`NORNICDB_PERSIST_SEARCH_INDEXES=true` and `NORNICDB_EMBEDDING_ENABLED=false`
for normal Eshu graph-only runs.

## What To Record Before Changing Knobs

Record this in the PR, runbook, or evidence note:

- repository or corpus size.
- backend and backend version.
- schema/bootstrap state.
- clean-volume or warm-volume state.
- stage timing: collector complete, projection complete, queue-zero.
- queue pending, retrying, failed, and dead-letter counts.
- worker, batch, timeout, and graph backend env vars.
- relevant metrics, traces, or logs.

For performance-affecting code changes, use the versioned evidence markers
required by `scripts/verify-performance-evidence.sh`.
