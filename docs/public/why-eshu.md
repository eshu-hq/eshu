# Why Eshu

Eshu answers questions that cross code, infrastructure, deployment, runtime, and
documentation boundaries.

Code search can find a function. Terraform can show a resource. GitOps can show
a deployment object. Eshu connects that evidence so you can ask what depends on
what, where a service really runs, and what might break when you change it.

## What Eshu Builds

Eshu indexes repositories and external collector evidence into a self-hosted
code-to-cloud context graph.

It can ingest:

- source symbols, imports, calls, references, complexity, and dead-code
  candidates
- Terraform/HCL, Kubernetes, Helm, Kustomize, CloudFormation, ArgoCD,
  Crossplane, and related configuration
- package, OCI registry, Terraform state, AWS, workflow, and documentation
  facts when those collectors are enabled
- cross-repo references such as module sources, image names, repository URLs,
  deployment config, and shared resources

The same model is exposed through the CLI, MCP tools, and HTTP API.

## Questions It Helps With

- Where is this function, class, workload, Terraform resource, or deployment
  config defined?
- Who calls this code, directly or transitively?
- What service deploys this image?
- Which workloads share this database, queue, bucket, secret, or cloud resource?
- What repo and Terraform module own this cloud resource?
- What differs between staging and production?
- What is the blast radius of changing this service, module, API, or config?
- Which source files or manifests prove a relationship?

Truth level depends on the indexed evidence and runtime profile. Query responses
label that boundary instead of pretending every answer is exact. See
[Truth Labels](reference/truth-label-protocol.md) and
[Capability Conformance](reference/capability-conformance-spec.md).

## Architecture In One Pass

- collectors observe source truth and emit versioned facts
- Postgres stores facts, queues, content, status, and recovery state
- reducers admit evidence, correlate it, and materialize graph truth
- NornicDB is the default graph backend; Neo4j is compatibility-only behind the
  same ports
- handlers and MCP tools read through bounded query contracts

Backends are supported only when they satisfy Eshu's capability contract for the
target profile. Start with [Architecture](architecture.md),
[Backend Conformance](reference/backend-conformance.md), and
[Graph Backend Operations](reference/graph-backend-operations.md).

## Who Uses It

| Team | Common use |
| --- | --- |
| Software engineers | Callers, callees, implementations, dependency paths, dead-code candidates, complexity, and change surface. |
| Platform and SRE | Workload dependencies, deployment paths, shared infrastructure, and environment drift. |
| Security and compliance | Resource consumers, infrastructure ownership, and evidence-backed audit paths. |
| Architects and tech leads | Cross-repo migration, refactor, and platform-shape planning. |
| New engineers | Service context without interrupting the team that owns it. |

## Why MCP Matters

MCP lets a coding assistant ask Eshu for indexed evidence instead of guessing
from the current file. Use it for questions like:

- "Who calls this function across indexed repos?"
- "What workload uses this queue in prod?"
- "Trace this RDS instance back to Terraform and source code."
- "What changes if I modify this service?"

Start with [MCP Guide](guides/mcp-guide.md) and
[Starter Prompts](guides/starter-prompts.md).

## Start Here

- [Start Here](start-here.md)
- [Run Locally](run-locally/index.md)
- [Index Repositories](use/index-repositories.md)
- [MCP Guide](guides/mcp-guide.md)
- [HTTP API](reference/http-api.md)
- [Use Cases](use-cases.md)
