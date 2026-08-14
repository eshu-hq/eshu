<!-- docs-catalog
title: Complete a local Compose first run
description: Guides a new reader through one local Compose runtime, one indexed repository, and one bounded repository-list result.
type: tutorial
audience: new-user
time: Varies with repository size
entrypoint: true
landing: false
-->

# Complete a local Compose first run

Use this tutorial to start Eshu with Docker Compose, index one repository, and
confirm that the Compose service returns a bounded repository list.

## What you will accomplish

By the end, the local stack will be running, the `eshu` repository will be
indexed, and the guided check will have returned a bounded repository-list
result.

## Before you begin

Indexing time varies with repository size, enabled indexing features, and the local
machine. Continue from status evidence rather than an elapsed-time estimate.
You need:

- Docker Compose.
- The Eshu checkout at `$HOME/src/eshu` or an equivalent parent and repository
  path.

For a different runtime shape, use the
[first successful run chooser](../getting-started/first-successful-run.md).

## Start and verify Eshu

1. From the Eshu checkout, expose the parent directory and select the checkout
   by its directory name:

   ```bash
   export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
   export ESHU_REPOSITORY_RULES_JSON='{"exact":["eshu"]}'
   export ESHU_API_KEY="local-compose-token"
   docker compose up -d
   ```

   Replace `$HOME/src` if your checkout has a different parent directory. Keep
   `eshu` in the selector only when that is the checkout's directory name.
2. Compose waits for the one-shot bootstrap before it starts dependent
   services. Before running another indexing command, verify that the completed
   index is reusable:

   ```bash
   docker compose ps --all bootstrap-index
   docker compose exec eshu eshu workspace status
   docker compose exec eshu eshu list
   ```

   Continue when bootstrap shows `Exited (0)` and the API is healthy. `workspace
   status` must report healthy state, no outstanding queue work, a
   completed or active generation, and no failed or dead-letter stage or domain work.
   `list` must contain `eshu` with `local_path` set to `/data/repos/eshu`; these
   are the signals `first-run` uses for reuse. Then run the guided check inside
   the Compose API service with that managed path:

   ```bash
   docker compose exec eshu eshu first-run /data/repos/eshu
   ```

   The command uses the same repository and stores as the running stack. It
   verifies Compose without starting it, then waits for indexing completeness
   and a bounded repository query.
3. Confirm the result from the same service container:

   ```bash
   docker compose exec eshu eshu index-status
   docker compose exec eshu eshu list
   docker compose exec eshu eshu stats eshu
   ```

## Verify the result

The container-executed `index-status` command should show a drained index, and
`list` should include the selected repository. The guided check should report
success only after its bounded repository-list query returns. If it does not,
wait for the index to drain and rerun it.

## Troubleshoot the run

- If the API is unreachable, run `docker compose ps`, fix unhealthy services,
  and rerun the container-executed guided check.
- If the repository is missing, check `ESHU_FILESYSTEM_HOST_ROOT`, the Docker
  mount at `/fixtures`, and the exact directory name in
  `ESHU_REPOSITORY_RULES_JSON`.
- If the API returns 401 or 403, make the client and server `ESHU_API_KEY`
  values match.
- If health is green but the repository result is stale, wait for
  `eshu index-status` to drain. Health alone does not prove indexing readiness.

## Clean up

If you started Compose for this tutorial, stop it from the same checkout:

```bash
docker compose down
```

Leave the stack running if it was already in use before the tutorial.

## Read next

- [Ask Eshu from an assistant](ask-from-assistant.md)
- [Debug stale answers](debug-stale-answers.md)
- [Index repositories](../use/index-repositories.md)
