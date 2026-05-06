# Use Eshu

Use these pages once Eshu is running locally or deployed for a team.

Eshu is built for questions that cross code, deployment, and infrastructure
boundaries. Start with one concrete thing: a repository, function, workload,
deployment object, cloud resource, or environment.

## Common workflows

| Goal | Start here |
| --- | --- |
| Add repository data to Eshu | [Index repositories](index-repositories.md) |
| Ask code questions | [Ask code questions](code-questions.md) |
| Trace workloads and infrastructure | [Trace infrastructure](trace-infrastructure.md) |
| Use an assistant | [Connect MCP](../mcp/index.md) |

## Interfaces

Eshu exposes the same model through three interfaces:

- CLI for local indexing and common analysis commands
- MCP for AI assistants and IDE workflows
- HTTP API for automation and internal platforms

The important distinction: `eshu index` writes through the configured graph and
Postgres stores, while commands such as `eshu list`, `eshu stats`, and
`eshu analyze ...` call the API.

## Useful first questions

- "Who calls `process_payment`?"
- "What code references this queue?"
- "What deploys this service?"
- "Which workloads share this database?"
- "What changes if this Terraform module changes?"
