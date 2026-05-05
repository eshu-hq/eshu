# Understand Eshu

Use this section when you want the system model, not the setup path.

Eshu has a facts-first runtime:

1. ingester discovers repositories and parses snapshots
2. facts are stored in Postgres
3. reducer claims queued work
4. graph and content projections are written
5. CLI, MCP, and HTTP API read the resulting model

## Read Next

| Topic | Read |
| --- | --- |
| End-to-end pipeline | [How It Works](../concepts/how-it-works.md) |
| Runtime and storage boundaries | [Architecture](../architecture.md) |
| Nodes, edges, and identity rules | [Graph Model](../concepts/graph-model.md) |
| Interfaces, profiles, and modes | [Modes](../concepts/modes.md) |
| Service-by-service workflow | [Service Workflows](../reference/service-workflows.md) |
| Truth levels | [Truth Label Protocol](../reference/truth-label-protocol.md) |
| Capability support by profile | [Capability Conformance Spec](../reference/capability-conformance-spec.md) |

## Concepts To Keep Separate

- **Health**: whether a process is alive and ready.
- **Completeness**: whether indexed state is fresh enough for the question.
- **Truth level**: whether an answer is exact, derived, fallback, or
  unsupported for the active profile.
- **Backend**: NornicDB or Neo4j behind the graph ports.

Handlers depend on graph and content ports, not concrete backend code.
