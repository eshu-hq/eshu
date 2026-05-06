# Eshu

Eshu connects code, infrastructure, workloads, and
deployment topology into one queryable graph. Engineers use it to ask questions
that usually require opening several repositories and chasing context by hand.

[Start Here](start-here.md){ .md-button .md-button--primary }
[Run Locally](run-locally/index.md){ .md-button }
[Deploy to Kubernetes](deploy/kubernetes/index.md){ .md-button }
[Connect MCP](mcp/index.md){ .md-button }

## Pick a path

### Run Eshu on your laptop

Start here if you want to try Eshu against one repo, develop Eshu itself, or run
the full service stack locally.

- [Choose your local path](run-locally/index.md)
- [Local binaries](run-locally/local-binaries.md)
- [Docker Compose](run-locally/docker-compose.md)

### Connect an assistant

Start here if you want Claude, Cursor, VS Code, or another MCP client to query
Eshu while you work.

- [MCP overview](mcp/index.md)
- [MCP guide](guides/mcp-guide.md)
- [Starter prompts](guides/starter-prompts.md)

### Deploy Eshu for a team

Start here if you are a DevOps or platform engineer deploying Eshu to
Kubernetes.

- [Kubernetes overview](deploy/kubernetes/index.md)
- [Storage requirements](deploy/kubernetes/storage.md)
- [Helm quickstart](deploy/kubernetes/helm-quickstart.md)
- [Production checklist](deploy/kubernetes/production-checklist.md)

## What Eshu helps answer

- "Who calls `process_payment` across all indexed repos?"
- "What deploys this service into QA and prod?"
- "Which workloads share this database?"
- "Trace this cloud resource back to the Terraform that defines it."
- "What breaks if I change this shared client?"

## What runs behind it

Eshu uses Postgres for relational state, facts, queues, status, content, and
recovery data. It uses NornicDB by default for graph storage, with Neo4j as the
explicit official alternative.

That backend choice is intentional. NornicDB is the default first-class graph
backend for Eshu. Neo4j is also supported as a first-class compatibility path for
teams that already operate Neo4j or want to keep using Neo4j tooling. Both
backends run Eshu's shared Cypher/Bolt graph contract, so the API, MCP, CLI,
ingester, and reducer do not fork into separate products.

The shared service shape has an API, MCP server, ingester, reducer, bootstrap
indexer, Postgres, and a graph backend. The local binary path is smaller: it
starts one local Eshu service with embedded Postgres, embedded NornicDB,
ingester, and reducer.

## Learn the system

Once you know which path you are on, the deeper docs are easier to use:

- [Use Eshu](use/index.md)
- [Operate Eshu](operate/index.md)
- [Understand Eshu](understand/index.md)
- [Extend Eshu](extend/index.md)
- [Reference](reference/cli-reference.md)
