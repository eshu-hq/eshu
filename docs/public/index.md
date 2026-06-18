# Eshu

Eshu connects code, dependencies, container images, infrastructure, workloads,
deployment topology, external collector facts, and documentation into one
queryable graph. It is the durable institutional-knowledge layer engineers and
AI assistants query instead of re-deriving an answer from several repositories,
dashboards, Terraform modules, and Helm charts by hand.

The launch beachhead is supply-chain traceability: trace a vulnerable dependency
through the images that ship it, the workloads that run them, and the source and
Terraform that own them — with evidence at every hop.

[First Successful Run](getting-started/first-successful-run.md){ .md-button .md-button--primary }
[Start Here](start-here.md){ .md-button }
[Run Locally](run-locally/index.md){ .md-button }
[Connect MCP](mcp/index.md){ .md-button }
[Deploy](deploy/kubernetes/index.md){ .md-button }

## Start With The Job You Have

| Job | Best first page |
| --- | --- |
| Get one working local run | [First successful run](getting-started/first-successful-run.md) |
| Try Eshu on a laptop | [Run locally](run-locally/index.md) |
| Connect an AI assistant | [Connect MCP](mcp/index.md) |
| Index repositories and ask questions | [Use Eshu](use/index.md) |
| Deploy a shared service for a team | [Deploy to Kubernetes](deploy/kubernetes/index.md) |
| Deploy on EKS | [Deploy to EKS](deploy/eks/index.md) |
| Operate or troubleshoot Eshu | [Operate Eshu](operate/index.md) |
| Understand the architecture | [Understand Eshu](understand/index.md) |
| Extend collectors or language support | [Extend Eshu](extend/index.md) |
| Code with an AI agent | [Code with agents](guides/coding-with-agents.md) |

## What Eshu Helps You Do

| Ability | Example question |
| --- | --- |
| Supply-chain traceability | "Which deployed images contain this vulnerable package, and what owns them?" |
| Code intelligence | "Who calls `process_payment` across indexed repos?" |
| Deployment tracing | "What deploys this service into QA and prod?" |
| Infrastructure ownership | "Trace this cloud resource back to the Terraform that defines it." |
| Shared dependency discovery | "Which workloads share this database, queue, or secret?" |
| Security and IAM posture | "What secrets and IAM roles can this workload reach?" |
| Change-risk analysis | "What breaks if I change this shared client?" |
| Assistant context | "Use Eshu to explain this service with source and deployment evidence." |

## First Concepts

Eshu has three user-facing interfaces:

- **CLI:** local setup, indexing, analysis, and operator commands.
- **MCP:** assistant-facing tools for Codex, Claude, Cursor, VS Code, and other
  MCP clients.
- **HTTP API:** automation and internal platform integration.

For local setup, start with [Run locally](run-locally/index.md). It explains
when to use Docker Compose and when to use local binaries from a checkout.

## Read Next

1. [First Successful Run](getting-started/first-successful-run.md) to start,
   index, check status, and ask one useful question.
2. [Start Here](start-here.md) to choose a path.
3. [Starter Prompts](guides/starter-prompts.md) for high-value questions once
   Eshu has indexed data.
