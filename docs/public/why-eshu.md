# Why Eshu

Eshu answers questions that cross code, dependencies, supply chain,
infrastructure, deployment, runtime, and documentation boundaries.

Code search can find a function. A package manifest can list a dependency.
Terraform can show a resource. GitOps can show a deployment object. Eshu
connects that evidence so you can ask what depends on what, where a service
really runs, which images carry a vulnerable dependency, and what might break
when you change it. It is the durable institutional-knowledge layer your team
and its assistants query instead of re-deriving the answer by hand.

## The Launch Beachhead: Supply-Chain Traceability

Eshu's first focused use case is following a dependency end to end. Start from a
vulnerable package and trace it through the container images that ship it, the
workloads that run those images, and the source and Terraform that own them —
with evidence at every hop. Eshu reconciles four advisory sources (CISA KEV,
FIRST EPSS, OSV, and NVD), follows SBOM-to-image attachments by subject digest,
and adds reachability so you can tell a dependency that is merely present from
one that is actually called. See
[Security Intelligence](reference/security-intelligence.md) and
[Vulnerability Scanner Confidence](reference/vulnerability-scanner-confidence.md).

## What Eshu Builds

Eshu indexes repositories and external collector evidence into a self-hosted
context graph that spans code, supply chain, infrastructure, and runtime.

It can ingest:

- source symbols, imports, calls, references, inheritance, complexity, and
  dead-code candidates across 20+ languages
- Terraform/HCL, Kubernetes, Helm, Kustomize, CloudFormation, ArgoCD,
  Crossplane, and Docker Compose configuration
- dependencies across 13 package ecosystems, container images from 7 registries
  (ECR, GHCR, Docker Hub, Harbor, GAR, ACR, JFrog), and SBOM-to-image
  attestations
- vulnerability intelligence from CISA KEV, FIRST EPSS, OSV, and NVD, plus
  govulncheck and value-flow reachability
- cloud inventory and posture from AWS service scanners, plus Terraform state,
  workflow, incident, observability, and documentation facts when those
  collectors are enabled
- cross-repo references such as module sources, image names, repository URLs,
  deployment config, and shared resources

The same model is exposed through the CLI, MCP tools, and HTTP API.

## Questions It Helps With

- Where is this function, class, workload, Terraform resource, or deployment
  config defined?
- Who calls this code, directly or transitively?
- Which deployed images contain this vulnerable package, and is it reachable?
- What service deploys this image?
- Which workloads share this database, queue, bucket, secret, or cloud resource?
- What secrets and IAM roles can this workload reach, and how?
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
| Platform and SRE | Workload dependencies, deployment paths, shared infrastructure, runtime drift, and environment drift. |
| Security and supply chain | Vulnerable-dependency reachability, SBOM-to-image attestations, hardcoded secrets, secret-access paths, and IAM trust and privilege chains. |
| Compliance and audit | Resource consumers, infrastructure ownership, and evidence-backed audit paths with source citations. |
| Architects and tech leads | Cross-repo migration, refactor, multi-cloud re-platforming, and platform-shape planning. |
| Engineering leadership | Change-risk and blast-radius visibility, capability coverage, and portfolio-wide ownership without opening every repo. |
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
