# Index Repositories

Indexing turns source code, infrastructure files, docs, and deployment config
into Eshu facts, content, and graph relationships.

## Choose An Indexing Path

| Path | Use it when |
| --- | --- |
| Docker Compose bootstrap | You want containers to index a mounted host directory while the full local stack starts. |
| Host CLI into Compose stores | You already have Compose running and want to index another checkout from your terminal. |
| Local Eshu service | You are using `eshu graph start` for one workspace-local development service. |

## Docker Compose Bootstrap

Compose indexes the host directory mounted at `/fixtures`. By default, that is
`./tests/fixtures/ecosystems`. Point it at a directory that contains the repos
you want indexed:

```bash
export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
export ESHU_REPOSITORY_RULES_JSON='{"exact":["payments-api","payments-infra"]}'

docker compose up --build
```

Use this path when you want the API, MCP server, ingester, reducer, Postgres,
and graph backend running together.

## Host CLI Into Compose Stores

Start Compose first:

```bash
docker compose up --build
```

Then point the host CLI and bootstrap binary at the Compose stores:

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

Use `scan` when you want Eshu to run bootstrap indexing and wait until the API
reports the pipeline is queryable:

```bash
eshu scan .
```

Use `index` when you only want to launch `eshu-bootstrap-index` and do not need
the readiness wait:

```bash
eshu index .
```

Both commands require `eshu-bootstrap-index` on `PATH`. From a source checkout,
the local installer builds it:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

If you use `docker-compose.neo4j.yml`, set `ESHU_GRAPH_BACKEND=neo4j` and use
database `neo4j` instead of `nornic`.

## Local Eshu Service

For one workspace-local development service, start the local owner:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"

eshu graph start --workspace-root "$PWD"
```

That path supervises embedded Postgres, embedded NornicDB, `eshu-ingester`, and
`eshu-reducer` for the workspace. See [Local binaries](../run-locally/local-binaries.md).

## Check Results

These commands call the HTTP API:

```bash
eshu index-status
eshu list
eshu stats
```

The default Compose API listens at `http://localhost:8080`.

## Exclude Local Noise

Eshu skips common cache and dependency directories by default, including `.git`,
`.terraform`, `.terragrunt-cache`, `.pulumi`, `node_modules`, `vendor`, and
`site-packages`.

Use `.eshuignore` for repo-specific generated files, local state, or large
fixtures that should not be indexed. See [.eshuignore](../reference/eshuignore.md).
