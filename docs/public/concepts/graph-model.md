# The Graph Model

Eshu uses an **entity-first graph** that represents code, workloads, infrastructure, and deployment context together.

## Canonical entities

Common node types include:

- **`Repository`**
- **`File`**
- **`Function`**
- **`Class`**
- **`Workload`**
- **`WorkloadInstance`**
- **`TerraformModule`**
- **`TerraformResource`**
- **`K8sResource`**
- **`CloudResource`**
- **`Image`**
- **`Endpoint`**
- **`Environment`**

## Relationship patterns

Some edges describe direct technical structure:

- `(:Function)-[:CALLS]->(:Function)`
- `(:Class)-[:INHERITS]->(:Class)`
- `(:File)-[:CONTAINS]->(:Function)`
- `(:Repository)-[:DEFINES]->(:Workload)`
- `(:Repository)-[:EXPOSES_ENDPOINT]->(:Endpoint)`
- `(:Workload)-[:EXPOSES_ENDPOINT]->(:Endpoint)`

Some edges describe deployable-system context:

- `(:Workload)-[:HAS_INSTANCE]->(:WorkloadInstance)`
- `(:WorkloadInstance)-[:RUNS_IMAGE]->(:Image)`
- `(:WorkloadInstance)-[:DEPLOYED_AS]->(:K8sResource)`
- `(:TerraformModule)-[:DEFINES]->(:TerraformResource)`
- `(:TerraformResource)-[:PROVISIONS]->(:CloudResource)`
- `(:WorkloadInstance)-[:USES]->(:CloudResource)`

## Repository identity

Repository nodes are remote-first when a git remote exists.

- Canonical identity is derived from normalized remote identity when available.
- `repo_slug` and `remote_url` identify the logical repository across different checkouts.
- `local_path` records where the Eshu server indexed that repository on disk.
- File-bearing API and MCP results should be treated as `repo_id + relative_path`, not as portable absolute paths.

## Content identity

Content-bearing entities also have canonical IDs.

- file content is addressed with `repo_id + relative_path`
- entity content is addressed with `entity_id`
- the content store keeps indexed file text and cached entity snippets in Postgres
- the service falls back to the server workspace when Postgres is absent or missing a row

## Why this matters

This model lets Eshu answer both:

- code-only questions like callers, callees, imports, or dead code
- code-to-cloud questions like "what workloads use this shared RDS cluster?"

For the broader concept path, start with [Understand Eshu](../understand/index.md).

## Example query

```cypher
MATCH (w:WorkloadInstance)-[:USES]->(db:CloudResource)
WHERE db.name = 'shared-payments-prod'
RETURN w.name, w.environment
```
