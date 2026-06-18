# How It Works

Eshu turns your repositories and external sources into a queryable graph through
a fact-based pipeline:
`sync → discover → parse → emit facts → enqueue → reduce → project → query`.
Facts are the durable boundary between intake and the graph, so work can be
retried, replayed, and recovered. Understanding the pipeline helps you ask
better questions and interpret the answers.

## 1. Discovery

Eshu syncs each repository, then walks its file tree honoring `.gitignore` and `.eshuignore`. Hidden and cache directories (`.git`, `.terraform`, `.terragrunt-cache`, `node_modules`, `vendor/`) are pruned automatically. What remains is the set of files that represent your actual code and infrastructure.

## 2. Parsing

Each file is routed to the appropriate parser:

- **Source code** — tree-sitter and native Go grammars extract functions, classes, imports, inheritance, and call relationships across 20+ languages
- **Terraform / HCL** — dedicated HCL parser extracts resources, modules, variables, and module source references
- **Kubernetes / Helm / Kustomize** — YAML parser identifies workloads, services, config maps, and overlay relationships
- **ArgoCD** — Application and ApplicationSet manifests are parsed to extract deployment targets, sync policies, and source repos
- **Crossplane** — XRD and Claim definitions are extracted to map infrastructure provisioning
- **CloudFormation / Docker Compose** — resource, output, and service definitions are parsed from JSON/YAML templates

Language parsing is owned by native Go packages. Add new parser capability by
extending the Go parser or relationship packages with fixtures and focused
tests.

## 3. Fact emission and external collectors

Parsers emit versioned facts rather than writing the graph directly. External
collectors emit facts too, so the graph reaches beyond the repository tree:
cloud inventory and posture from AWS service scanners, container images from
registries, dependencies from package ecosystems, vulnerability intelligence
(CISA KEV, FIRST EPSS, OSV, NVD), Kubernetes runtime, observability, CI/CD, and
incident sources. Every fact is generation-stamped so a later run can supersede
an earlier one without losing provenance.

## 4. Queue and reduce

Facts are stored in Postgres and queued as reducer work. The reducer (the
resolution engine) claims that work, admits each fact into canonical state or
keeps it as provenance, and **correlates** evidence across sources — connecting a
Terraform module source URL to the repository that contains it, or tracing an
ArgoCD Application through Kubernetes resources to the image and source that
define a workload. This correlation layer is what makes Eshu different from a
code search tool. Queued, idempotent work is what lets it run concurrently and
recover from partial failure.

## 5. Graph and content projection

The reducer materializes canonical nodes and edges, gating edge writes behind
readiness phases so endpoints exist before an edge is merged:

- **NornicDB** — default graph backend for local and deployable service paths
- **Neo4j** — explicit Bolt-compatible official alternative
- **PostgreSQL** — content store for source text retrieval and full-text search

Direct facts (files, functions, Terraform resources, K8s manifests, images,
packages) and inferred relationships (deployment chains, shared-infrastructure
consumption, code-to-cloud handler and runtime bridges) land in the same model.
All three query interfaces (CLI, MCP, HTTP) read from the same storage layer.

## 6. Querying

Queries resolve user-friendly input ("payment-service", "shared-rds-cluster") into canonical graph entities — repositories, workloads, workload instances, cloud resources — and traverse the graph from there.

A concrete example: `trace_deployment_chain payment-service` resolves "payment-service" to its ArgoCD Application, walks to the K8s Deployment, finds the container image reference, maps that image to a repository, and returns the full chain with evidence at each hop.

The same query model works across CLI, MCP, and HTTP API — same capabilities, same results, different interfaces.

## Next steps

- [Understand Eshu](../understand/index.md) — the concept entry path
- [Architecture](../architecture.md) — deeper technical detail on graph schema and query resolution
- [Interfaces](modes.md) — choosing between CLI, MCP, and HTTP
- [Fixture Ecosystems](../guides/fixture-ecosystems.md) — test data that exercises this pipeline
