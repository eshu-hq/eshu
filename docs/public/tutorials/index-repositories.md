# Tutorial: Index Repositories

Use this tutorial when you want Eshu to ingest source code, infrastructure
files, docs, and deployment config from one or more repositories.

## Outcome

Eshu has indexed your selected repositories and can report repository status,
counts, and basic facts.

## Time

About 10-20 minutes for a small local repository set, depending on repository
size and machine speed.

## Prerequisites

- A local Eshu runtime or Compose stack.
- Repository paths available on the host.
- Local helper binaries on `PATH` when using host CLI commands.

## Steps

1. Choose a parent directory that contains the repositories:

   ```bash
   export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
   ```

2. Select the repositories by directory name:

   ```bash
   export ESHU_REPOSITORY_RULES_JSON='{"exact":["payments-api","payments-infra"]}'
   ```

3. Start the full local stack:

   ```bash
   docker compose up --build
   ```

4. In another terminal, point the host CLI at the Compose stores if you want to
   launch another scan from your shell:

   ```bash
   export ESHU_GRAPH_BACKEND=nornicdb
   export NEO4J_URI=bolt://localhost:7687
   export NEO4J_USERNAME=neo4j
   export NEO4J_PASSWORD=change-me
   export DEFAULT_DATABASE=nornic
   export ESHU_NEO4J_DATABASE=nornic
   export ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu
   export ESHU_CONTENT_STORE_DSN=postgresql://eshu:change-me@localhost:15432/eshu
   ```

5. Run a scan and wait for readiness:

   ```bash
   eshu scan .
   ```

6. Check the results:

   ```bash
   eshu index-status
   eshu list
   eshu stats
   ```

## Expected Result

`eshu index-status` shows indexing progress or completion, `eshu list` includes
the selected repositories, and `eshu stats` returns counts for indexed content.
The exact counts depend on the repository set.

## Failure Hints

- If repositories are missing, confirm each selected directory exists under
  `ESHU_FILESYSTEM_HOST_ROOT`.
- If Compose cannot mount the path, use an absolute non-symlinked path visible
  to Docker.
- If indexing is slow, create a discovery advisory before changing timeouts or
  worker counts.
- If generated or vendored files dominate the scan, add `.eshuignore` rules.

## Read Next

- [Index Repositories](../use/index-repositories.md) for all indexing paths.
- [.eshuignore](../reference/eshuignore.md) for excluding local noise.
- [Debug stale answers](debug-stale-answers.md) when the index exists but
  answers are not fresh.
