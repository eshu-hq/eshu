<!-- docs-catalog
title: Debug Stale Answers
description: Diagnoses why an Eshu answer is missing, stale, or less complete than expected.
type: tutorial
audience: operator, practitioner
time: 10 minutes
entrypoint: true
landing: false
-->

# Tutorial: Debug Stale Answers

Use this tutorial when Eshu is reachable but an answer is missing, stale, or
less complete than expected.

## Outcome

You identify whether the issue is process health, repository discovery,
ingestion, queue processing, graph writes, or assistant/client configuration.

## Time

About 10 minutes for the first diagnosis.

## Prerequisites

- A running local or hosted Eshu runtime.
- API access with a valid token when auth is enabled.
- A repository, service, workload, or question that produced the stale answer.

## Steps

1. Separate process health from data readiness:

   ```bash
   curl -fsS http://localhost:8080/healthz
   curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
     http://localhost:8080/api/v0/index-status
   ```

2. Check status surfaces that report indexing and runtime progress:

   ```bash
   curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
     http://localhost:8080/api/v0/status/index
   curl -fsS http://localhost:8080/admin/status
   ```

3. If repositories are missing, verify discovery and mount paths:
   - `ESHU_FILESYSTEM_HOST_ROOT` is absolute.
   - The selected repository has a `.git` directory.
   - Docker can see the mounted parent directory.
4. If indexing is not moving, inspect ingester logs and repository access.
5. If queue depth or oldest age keeps rising, inspect the reducer or
   resolution-engine logs.
6. If only assistant answers are stale, verify the MCP client points to the MCP
   service and not the HTTP API endpoint.

## Expected Result

The status endpoints should show whether Eshu is still building, complete,
partial, stale, or failed. A healthy API with a stale index points to ingestion
or reducer work, not to API process health.

## Failure Hints

- Do not restart the API just because an answer is stale; the API may be serving
  correctly while data processing is behind.
- If queue depth falls over time, wait and re-check before changing worker
  settings.
- If graph-backed queries fail, check `ESHU_GRAPH_BACKEND` and graph backend
  health.
- If Compose cannot see repos on macOS, avoid `/tmp` and symlinked paths.

## Read Next

- [Troubleshooting](../operate/troubleshooting.md) for the full symptom table.
- [Health Checks](../operate/health-checks.md) for readiness and liveness
  details.
- [Index Repositories](index-repositories.md) if the repository never entered
  the index.
