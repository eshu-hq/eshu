# Deploy to Kubernetes

Use this path when Eshu will run as a shared service for a team.

The Helm chart is the supported Kubernetes entry point. It renders separate
workloads for the API, MCP server, ingester, optional workflow coordinator, and
resolution engine. It can also render separate optional collectors for
Confluence documentation, OCI registries, and public GitHub, GitLab, or
Bitbucket default-branch refresh triggers. Every database-backed workload also
runs the `eshu-bootstrap-data-plane` init container before the main process
starts.

## What gets deployed

| Workload | Kubernetes shape | Purpose |
| --- | --- | --- |
| API | `Deployment` | Serves HTTP query, admin, health, and metrics traffic. |
| MCP server | `Deployment` | Serves MCP transport and mounted query routes. |
| Ingester | `StatefulSet` | Owns repository sync, parsing, fact emission, and the workspace PVC. |
| Workflow coordinator | `Deployment` | Optional dark-mode control-plane validation. Disabled by default. |
| Confluence collector | `Deployment` | Optional documentation collector that writes section facts to Postgres. |
| Webhook listener | `Deployment` | Optional public webhook intake that writes targeted refresh triggers to Postgres. |
| OCI registry collector | `Deployment` | Optional registry scanner that writes digest-addressed image facts to Postgres. |
| Resolution engine | `Deployment` | Drains queued projection work and writes canonical graph state. |
| DB migrate | `initContainer` | Applies Postgres and graph schema DDL before each workload starts. |

The chart does not install Postgres, NornicDB, or Neo4j. Bring those storage
systems first, then point the chart at them with Helm values.

## Default backend

NornicDB is the default graph backend. Neo4j is the explicit official
alternative. The chart value names still use `neo4j.*` for the Bolt
connection because both supported backends use the Neo4j driver shape.

Default backend selection is not the same as closing every promotion gate.
NornicDB has latest-main full-corpus evidence, and the accepted Neo4j parity
ADR records the schema-first proof for the official alternative. The remaining
promotional work is install-trust policy and broader host coverage.

Unsupported graph backends are not official.

## Read next

1. [Prerequisites](prerequisites.md) for cluster, secret, and tool requirements.
2. [Storage](storage.md) for Postgres, NornicDB, and Neo4j requirements.
3. [Helm Quickstart](helm-quickstart.md) for the install flow.
4. [Production Checklist](production-checklist.md) before exposing the service.
