# Docker Compose

Use Docker Compose when you want the full Eshu service stack on your laptop.
This is the easiest local path for trying API, MCP, ingestion, reduction,
Postgres, and the graph backend together.

## Start the default stack

From the repository root:

```bash
docker compose up --build
```

The default stack uses NornicDB for graph storage and Postgres for relational
state, facts, queues, status, content, and recovery data.

It starts:

- NornicDB
- Postgres
- API
- MCP server
- ingester
- reducer
- bootstrap indexer

## Start the Neo4j stack

Neo4j is the explicit official alternative graph backend:

```bash
docker compose -f docker-compose.neo4j.yml up --build
```

Use this when you need to validate Neo4j behavior or migrate an existing Neo4j
deployment path.

## Add local telemetry

Jaeger and the OpenTelemetry collector are not part of the default Compose
files. Use the telemetry overlay when you want a local collector and trace UI
for developer or DevOps testing:

```bash
docker compose -f docker-compose.yaml -f docker-compose.telemetry.yml up --build
```

For Neo4j with the same local telemetry stack:

```bash
docker compose -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml up --build
```

The overlay adds:

- OpenTelemetry collector
- Jaeger
- OTLP trace and metric export settings for the Eshu runtimes

## Run the workflow coordinator proof profile

The workflow coordinator profile is off by default. It is useful when you want
to inspect the control plane or run an active claim proof without changing the
normal ingester path.

Dark-mode status proof:

```bash
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

Active claim proof requires every guard to be explicit:

```bash
export ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active
export ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true
export ESHU_COLLECTOR_INSTANCES_JSON='[{"instance_id":"collector-git-proof","collector_kind":"git","mode":"continuous","enabled":true,"bootstrap":true,"claims_enabled":true,"configuration":{"source":"local-compose","fairness_weight":1}}]'
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

Use active mode only for fenced claim validation. The Kubernetes chart remains
dark-only until the remote full-corpus proof, API checks, MCP checks, and
evidence truth checks are clean.

## Point local CLI commands at Compose

The API is available at `http://localhost:8080` by default. For indexing into
the default NornicDB Compose stack, use:

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

Then index and query:

```bash
eshu index .
eshu list
eshu analyze callers process_payment
```

If you use `docker-compose.neo4j.yml`, set `ESHU_GRAPH_BACKEND=neo4j` and use
database `neo4j` instead of `nornic`.

## Local endpoints

- API: `http://localhost:8080`
- MCP server: `http://localhost:8081`
- Postgres: `localhost:15432`
- Graph Bolt endpoint: `localhost:7687`

When the telemetry overlay is enabled, Jaeger is available at
`http://localhost:16686`.

See [Connect MCP locally](mcp-local.md) for MCP client setup.
