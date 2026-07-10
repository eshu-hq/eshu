<!-- docs-catalog
title: Use Eshu
description: Routes readers to task-focused guides after Eshu is running locally or for a team.
type: how-to
audience: practitioner
entrypoint: false
landing: true
-->

# Use Eshu

Use these pages after Eshu is running locally or deployed for a team.

Start with one concrete thing: a repository, function, workload, deployment
object, cloud resource, or environment.

## Common Workflows

| Goal | Start here |
| --- | --- |
| Add repository data | [Index repositories](index-repositories.md) |
| Ask code questions | [Ask code questions](code-questions.md) |
| Trace workloads and infrastructure | [Trace infrastructure](trace-infrastructure.md) |
| Ask through an assistant | [Connect MCP](../mcp/index.md) |
| Use ready-made prompts | [Starter Prompts](../guides/starter-prompts.md) |

## Interfaces

Eshu exposes the same indexed model through three interfaces:

- CLI for local indexing and operator-friendly commands.
- MCP for AI assistants and IDE workflows.
- HTTP API for automation and internal platforms.

CLI read commands use the HTTP API. That includes `eshu list`, `eshu stats`,
`eshu index-status`, `eshu find ...`, `eshu analyze ...`, `eshu map`, and
`eshu trace service ...`.

Indexing commands are different:

- `eshu scan` runs bootstrap indexing and waits for readiness through the API.
- `eshu index` launches `eshu-bootstrap-index` without the readiness wait.

## Useful First Questions

- "Who calls `process_payment`?"
- "What code references this queue?"
- "What deploys this service?"
- "Which workloads share this database?"
- "What changes if this Terraform module changes?"
