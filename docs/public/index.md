# Eshu

Eshu connects code, infrastructure, workloads, deployment topology, and
documentation into one queryable graph. Engineers use it when an answer would
otherwise require opening several repositories and chasing context by hand.

[Start Here](start-here.md){ .md-button .md-button--primary }
[Run Locally](run-locally/index.md){ .md-button }
[Connect MCP](mcp/index.md){ .md-button }
[Deploy](deploy/kubernetes/index.md){ .md-button }

## Start With The Job You Have

| Job | Best first page |
| --- | --- |
| Try Eshu on a laptop | [Run locally](run-locally/index.md) |
| Connect an AI assistant | [Connect MCP](mcp/index.md) |
| Index repositories and ask questions | [Use Eshu](use/index.md) |
| Deploy a shared service for a team | [Deploy to Kubernetes](deploy/kubernetes/index.md) |
| Operate or troubleshoot Eshu | [Operate Eshu](operate/index.md) |
| Understand the architecture | [Understand Eshu](understand/index.md) |
| Extend collectors or language support | [Extend Eshu](extend/index.md) |

## What Eshu Helps Answer

- "Who calls `process_payment` across all indexed repos?"
- "What deploys this service into QA and prod?"
- "Which workloads share this database?"
- "Trace this cloud resource back to the Terraform that defines it."
- "What breaks if I change this shared client?"

## First Concepts

Eshu has three user-facing interfaces:

- **CLI:** local setup, indexing, analysis, and operator commands.
- **MCP:** assistant-facing tools for Codex, Claude, Cursor, VS Code, and other
  MCP clients.
- **HTTP API:** automation and internal platform integration.

For local setup, start with [Run locally](run-locally/index.md). It explains
when to use Docker Compose and when to use local binaries from a checkout.

## Read Next

1. [Start Here](start-here.md) to choose a path.
2. [Run Locally](run-locally/index.md) to start Eshu.
3. [Starter Prompts](guides/starter-prompts.md) for high-value questions once
   Eshu has indexed data.
