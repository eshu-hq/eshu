<!-- docs-catalog
title: Index Repositories
description: Explains the local and Compose paths for indexing source, infrastructure, and docs repositories.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Index Repositories

Indexing turns source code, infrastructure files, docs, and deployment config
into Eshu facts, content, and graph relationships.

## Choose A Path

| Path | Use it when |
| --- | --- |
| Docker Compose bootstrap | You want the full local stack to index a mounted directory on startup. |
| Host CLI into Compose stores | Compose is already running and you want to index another checkout from your terminal. |
| Local Eshu service | You are using `eshu graph start` for one workspace-local development service. |

## Docker Compose Bootstrap

```bash
export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"
export ESHU_REPOSITORY_RULES_JSON='{"exact":["payments-api","payments-infra"]}'

docker compose up --build
```

Use this when you want API, MCP, ingester, reducer, Postgres, and graph backend
running together.

## Host CLI Into Compose Stores

Start Compose:

```bash
docker compose up --build
```

Point the host CLI at the Compose stores:

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

Use `scan` when you want the readiness wait:

```bash
eshu scan .
```

Use `index` when you only need to launch `eshu-bootstrap-index`:

```bash
eshu index .
```

Both commands require local binaries on `PATH`:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

If you use `docker-compose.neo4j.yml`, set `ESHU_GRAPH_BACKEND=neo4j` and use
database `neo4j`.

## Local Eshu Service

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"

eshu graph start --workspace-root "$PWD"
```

See [Local binaries](../run-locally/local-binaries.md).

## Check Results

```bash
eshu index-status
eshu list
eshu stats
```

Compose defaults to API `http://localhost:8080`.

## Exclude Local Noise

Eshu skips common cache and dependency directories by default, including `.git`,
`.terraform`, `.terragrunt-cache`, `.pulumi`, `node_modules`, `vendor`, and
`site-packages`.

Use [.eshuignore](../reference/eshuignore.md) for repo-specific generated files,
local state, or large fixtures that should not be indexed.
