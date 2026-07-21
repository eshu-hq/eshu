# The Graph Model

Eshu uses an **entity-first graph** that represents code, workloads, infrastructure, and deployment context together.

## Canonical entities

The graph projects dozens of node labels grouped by domain. The lists here are
representative, not exhaustive; the authoritative label set lives in
`go/internal/graph/schema.go` and `go/internal/projector/canonical.go`.

**Code and documentation:**

- **`Repository`**, **`File`**, **`Function`**, **`Class`**, **`Interface`**,
  **`Variable`**, **`Module`**
- **`DocumentationSection`** (identity-only; section body stays in Postgres)
- **`Rationale`** (identity-only; intent-comment text stays in Postgres)

**Workloads and deployment:**

- **`Workload`**, **`WorkloadInstance`**, **`K8sResource`**, **`Endpoint`**,
  **`Environment`**
- **`ArgoCDApplication`**, **`HelmChart`**, **`KustomizeOverlay`**,
  **`CrossplaneXRD`**

**Infrastructure:**

- **`TerraformModule`**, **`TerraformResource`** (config-declared, from parsed
  `.tf` files), **`TerraformStateResource`** (state-observed, from a
  Terraform state backend; matched to its declaring `TerraformResource` by a
  `MATCHES_STATE` edge only when all three hold: the state backend resolves
  to exactly one owning config repository, the state address exactly equals
  the config-declared address (never normalized, so a
  module/count/for_each-expanded address such as
  `module.vpc.aws_instance.foo["us-east-1"]` stays applied-only), and exactly
  one `TerraformResource` in that repository declares that address --
  an unresolved owner or an ambiguous address match records no edge rather
  than guessing), **`TerraformDataSource`**, **`TerraformProvider`**,
  **`CloudFormationResource`**
- **`CloudResource`**, **`Platform`**, **`CloudAction`**, **`ExternalPrincipal`**

**Supply chain and security:**

- **`Image`**, **`ContainerImage`**, **`OciImageManifest`**,
  **`OciRegistryRepository`**
- **`Package`**, **`PackageVersion`**, **`PackageDependency`**
- secrets and IAM identity nodes such as **`SecretsIAMServiceAccount`**,
  **`SecretsIAMVaultPolicy`**, and **`SecretsIAMSecretMetadataPath`**

## Relationship patterns

Some edges describe direct technical structure:

- `(:Function)-[:CALLS]->(:Function)`
- `(:Class)-[:INHERITS]->(:Class)`
- `(:Class)-[:IMPLEMENTS]->(:Interface)`
- `(:Function)-[:INSTANTIATES]->(:Class)`
- `(:Rationale)-[:EXPLAINS]->(:Function)`
- `(:File)-[:CONTAINS]->(:Function)`
- `(:DocumentationSection)-[:DOCUMENTS]->(:Function)`
- `(:Repository)-[:DEFINES]->(:Workload)`
- `(:Repository)-[:EXPOSES_ENDPOINT]->(:Endpoint)`
- `(:Workload)-[:EXPOSES_ENDPOINT]->(:Endpoint)`

Some edges describe deployable-system context:

- `(:WorkloadInstance)-[:INSTANCE_OF]->(:Workload)`
- `(:WorkloadInstance)-[:RUNS_ON]->(:Platform)`
- `(:KubernetesWorkload)-[:RUNS_IMAGE]->(:OciImageManifest)`
- `(:WorkloadInstance)-[:USES]->(:CloudResource)`
- `(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)`

The graph also carries newer edge families that connect code to cloud and
capture supply-chain and security posture. These edge types are projected by the
reducer; their authoritative definitions live alongside the node labels in
`go/internal/graph` and the reducer projection packages:

- **Code-to-cloud bridges** — `(:Function)-[:HANDLES_ROUTE]->(:Endpoint)` (a
  handler function serves an endpoint), `(:Function)-[:RUNS_IN]->(:Workload)` (a
  route-handler function runs in the workload it is deployed in), and
  `(:Function)-[:INVOKES_CLOUD_ACTION]->(:CloudAction)` (code invokes a cloud
  action).
- **IAM reachability** — `CAN_ASSUME`, `CAN_ESCALATE_TO`, and `CAN_PERFORM`
  capture how identities reach roles, escalate privilege, and perform actions.
- **Supply chain** — package and dependency edges connect repositories and
  images to the packages they declare and depend on.
- **Data flow** — `REFERENCES` and `TAINT_FLOWS_TO` capture value flow used by
  reachability analysis.

## Code edge resolution provenance

`CALLS`, `REFERENCES`, and `USES_METACLASS` edges record **how** their target
entity was resolved, so an agent can tell a semantically proven edge from a
name-match guess. Each edge carries:

- `resolution_method` — a closed value from the vocabulary below.
- `confidence` — a float derived from `resolution_method` (never an independent
  signal).
- `reason` — a short mechanism-level explanation of the resolution.

| `resolution_method` | Meaning | `confidence` |
| --- | --- | --- |
| `scip` | SCIP semantic symbol resolution; both endpoints bound by symbol. | 0.99 |
| `declared` | Explicitly declared in source (e.g. Python metaclass); no heuristic resolution. | 0.95 |
| `same_file` | Resolved inside the caller's file by lexical scope or unique name. | 0.95 |
| `import_binding` | Resolved by following an explicit import, package-qualified import, or re-export. | 0.90 |
| `type_inferred` | Resolved by receiver/return-type inference, dynamic alias, or constructor binding. | 0.80 |
| `scope_unique_name` | Resolved by a unique name within a directory/package scope, no import. | 0.70 |
| `cross_repo_export_package` | Resolved across repositories by matching a Go package import path to the single exported function with that name. | 0.70 |
| `repo_unique_name` | Resolved by a repository-wide unique-name match; the global fallback. | 0.50 |

Provenance is **descriptive, not admissive**: it records the resolver branch
that produced an already-admitted edge and never changes which edges exist or
promotes a heuristic to canonical truth. It is also orthogonal to the
answer-level truth envelope — an `exact` answer can still contain a
`repo_unique_name` edge; the per-edge method flags that one edge as uncertain
without lowering the answer's truth level.

The fields are additive. Edges projected before this contract (or where the
method cannot be determined) read as `unspecified` and keep the historical
`0.95`; readers must treat a missing `resolution_method` as `unspecified`. The
vocabulary is fixed by
[design 2222](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/2222-resolution-provenance-code-edges.md);
the authoritative table lives in `go/internal/codeprovenance`.

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
