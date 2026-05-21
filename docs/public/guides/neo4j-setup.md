# Neo4j Setup

Use this guide when you want the deployable-service path or a more production-like local setup.

## `eshu neo4j setup`

The helper is meant to reduce the friction of getting Neo4j credentials and a reachable database into the Eshu runtime.

Typical behavior:

1. detect whether Docker or another supported setup path is available
2. help you connect to a local or remote Neo4j instance
3. persist the connection settings for local use

## What to verify after setup

- Neo4j is reachable on the configured URI
- the username and password are valid
- `eshu doctor` confirms the backend is healthy

## Common failure modes

- a local port conflict on `7687` or `7474`
- Docker not running when you expected a local container workflow
- wrong credentials for a remote Neo4j instance
- local MCP or HTTP runtime not receiving the same environment values you configured interactively

For Kubernetes deployments, the public contract is an **external
Bolt-compatible graph endpoint via secrets or environment values**, not an
in-chart database. Set `ESHU_GRAPH_BACKEND=neo4j` when that endpoint is Neo4j.
