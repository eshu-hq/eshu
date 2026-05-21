# Prerequisites

Before you install Eshu, decide which mode you want first:

- **Local CLI and MCP** for single-user development workflows
- **HTTP API + MCP service** for automation or team-shared usage
- **Kubernetes deployment** when you want bootstrap indexing, repo sync, and a long-running service

## What Eshu needs

### Go runtime

- Go 1.26 or newer if you are building from source
- or prebuilt Eshu binaries (`eshu`, `eshu-api`, `eshu-mcp-server`,
  `eshu-bootstrap-index`, `eshu-ingester`, `eshu-reducer`) available on `PATH`

### Graph database

Eshu uses a Bolt/Cypher-compatible graph backend plus Postgres. The officially
supported graph databases are NornicDB and Neo4j; Postgres is the supported
relational database for facts, queues, status, recovery state, and content.

| Backend | Best fit | Notes |
| --- | --- | --- |
| NornicDB | default local CLI/MCP and default Compose stack | Used by `eshu graph start` and `docker compose up` |
| Neo4j | explicit compatibility and migration path | Use `docker-compose.neo4j.yml` or set `ESHU_GRAPH_BACKEND=neo4j` |

### Optional infrastructure tooling

- Docker Desktop or Docker Engine for local service testing
- Kubernetes and Helm for cluster deployments
- Argo CD for GitOps-style deployments

## Platform notes

- **macOS / Linux:** local CLI and MCP setup is straightforward.
- **Windows:** use [Windows Setup](windows-setup.md) if you need WSL guidance or Neo4j-specific setup help.
- **Kubernetes:** the chart expects an external Bolt-compatible graph endpoint,
  external Postgres, and a persistent workspace volume for bootstrap indexing
  and repo sync.

## Before your first index

Make sure you know:

- which repository or mono-folder you want to index first
- whether you want code-only indexing or code plus infrastructure context
- whether you want to run over stdio MCP or expose the HTTP API as a service
