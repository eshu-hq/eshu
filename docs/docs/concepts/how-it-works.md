# How It Works

Eshu turns your repositories into a queryable graph in five steps. Understanding the pipeline helps you ask better questions and interpret the answers.

## 1. Discovery

Eshu walks the file tree of each repository, honoring `.gitignore` and `.eshuignore`. Hidden and cache directories (`.git`, `.terraform`, `.terragrunt-cache`, `node_modules`, `vendor/`) are pruned automatically. What remains is the set of files that represent your actual code and infrastructure.

## 2. Parsing

Each file is routed to the appropriate parser:

- **Source code** — tree-sitter grammars for 30+ languages extract functions, classes, imports, and call relationships
- **Terraform / HCL** — dedicated HCL parser extracts resources, modules, variables, and module source references
- **Kubernetes / Helm / Kustomize** — YAML parser identifies workloads, services, config maps, and overlay relationships
- **ArgoCD** — Application and ApplicationSet manifests are parsed to extract deployment targets, sync policies, and source repos
- **Crossplane** — XRD and Claim definitions are extracted to map infrastructure provisioning
- **CloudFormation** — resource and output definitions are parsed from JSON/YAML templates

Language parsing is owned by native Go packages. Add new parser capability by
extending the Go parser or relationship packages with fixtures and focused
tests.

## 3. Graph construction

Parser output becomes nodes and edges. Some are direct facts, some are inferred from multiple signals:

- **Direct facts** — files, functions, classes, imports, Terraform resources, K8s manifests, ArgoCD apps
- **Inferred relationships** — deployment chains (ArgoCD app → K8s Deployment → image → repo), shared infrastructure consumption (multiple workloads → same RDS module), service aliases, cross-repo module references

The inference layer is what makes Eshu different from a code search tool. It connects a Terraform module source URL to the repository that contains it, and traces an ArgoCD Application through K8s resources to the Helm chart and source code that define the workload.

## 4. Storage

The graph is written to the backing database:

- **NornicDB** — default graph backend for local and deployable service paths
- **Neo4j** — explicit Bolt-compatible official alternative
- **PostgreSQL** — content store for source text retrieval and full-text search

All three query interfaces (CLI, MCP, HTTP) read from the same storage layer.

## 5. Querying

Queries resolve user-friendly input ("payment-service", "shared-rds-cluster") into canonical graph entities — repositories, workloads, workload instances, cloud resources — and traverse the graph from there.

A concrete example: `trace_deployment_chain payment-service` resolves "payment-service" to its ArgoCD Application, walks to the K8s Deployment, finds the container image reference, maps that image to a repository, and returns the full chain with evidence at each hop.

The same query model works across CLI, MCP, and HTTP API — same capabilities, same results, different interfaces.

## Next steps

- [Understand Eshu](../understand/index.md) — the concept entry path
- [Architecture](../architecture.md) — deeper technical detail on graph schema and query resolution
- [Interfaces](modes.md) — choosing between CLI, MCP, and HTTP
- [Fixture Ecosystems](../guides/fixture-ecosystems.md) — test data that exercises this pipeline
