# Why Eshu

## The Problem

When you refactor a service, your editor sees the code in front of it. It does
not see the Terraform module that provisions the database, the ArgoCD
application that deploys the workload, the Kubernetes manifest that sets
replicas and secrets, or the other services that depend on the API contract.

That context exists, but it is spread across repositories, Helm charts,
Terraform modules, cloud accounts, deployment systems, and people. Engineers
rebuild the map by hand, skip the investigation, or discover the blast radius
after a change ships.

Code search finds text. Service catalogs track ownership. IaC tools manage
infrastructure. Eshu connects the evidence so teams can ask: **what connects to
what, and what breaks if I change it?**

## What Eshu Does

Eshu builds a self-hosted code-to-cloud context graph. It indexes source code,
infrastructure definitions, deployment configuration, runtime topology, and
documentation into one queryable model.

It observes:

- source code symbols, imports, references, call relationships, and complexity
- Terraform/HCL, Kubernetes, Helm, Kustomize, CloudFormation, and related IaC
- ArgoCD, Crossplane, workflow, package, OCI registry, Terraform state, and AWS
  collector evidence when those collectors are enabled
- cross-repo edges such as module sources, image references, repository URLs,
  deployment config, and shared resources

The same model is available through CLI commands, MCP tools for AI assistants,
and HTTP API routes for automation.

## What You Can Ask

Eshu is built for questions that cross normal repository boundaries:

- Where is this function, class, workload, Terraform resource, or deployment
  config defined?
- Who calls this function, and what code path reaches it?
- What service deploys this image?
- What workloads share this database, queue, bucket, secret, or cloud resource?
- What repo and Terraform module own this cloud resource?
- What differs between staging and production for this workload?
- What is the likely blast radius if this service, module, or API changes?
- Why are these two entities connected, and what evidence supports that link?

For code-specific queries, Eshu supports exact and fuzzy symbol lookup,
structure-aware language queries, direct and transitive call analysis,
dead-code candidates, complexity hotspots, import/reference lookup, and
content search. The exact truth level depends on the runtime profile and the
indexed evidence available. See [Truth Labels](reference/truth-label-protocol.md)
and [Capability Conformance](reference/capability-conformance-spec.md).

## What Makes It Different

Eshu treats infrastructure and deployment evidence as first-class graph inputs,
not annotations bolted onto code search. Collectors emit versioned facts.
Reducers admit relationships and materialize graph truth. Query surfaces return
bounded, evidence-carrying answers through the same CLI/MCP/API model.

That separation matters:

- collectors observe source truth and do not write graph truth directly
- reducers own admission, correlation, and shared projection
- Postgres stores facts, queues, content, status, and recovery state
- NornicDB is the default graph backend; Neo4j remains the compatibility backend
  behind the same capability ports
- handlers and tools depend on ports such as `GraphQuery` and `ContentStore`,
  not concrete database drivers

The backend contract is not "it speaks Cypher, so it is supported." Backends
must satisfy the capability matrix for the intended profile. See
[Architecture](architecture.md), [Backend Conformance](reference/backend-conformance.md),
and [Graph Backend Operations](reference/graph-backend-operations.md).

## Who It Helps

| Team | What Eshu helps answer |
| --- | --- |
| Software engineers | Callers, callees, implementation locations, dependency paths, dead code, complexity, and blast radius before a PR lands. |
| Platform / SRE | Which workloads depend on shared infrastructure, how services deploy, and what differs by environment. |
| Security and compliance | Which services touch a resource, where infrastructure is declared, and what evidence supports an audit path. |
| Architects and tech leads | How repositories connect across the ecosystem and where a migration or refactor will spread. |
| New engineers | What a repo or service does, how it deploys, and what it depends on without interrupting senior engineers. |

## Why MCP Matters

MCP puts the graph where engineers already ask questions: Codex, Claude,
Cursor, VS Code, and other MCP-compatible clients. Without MCP, Eshu is a useful
CLI and API. With MCP, an AI assistant can ask the graph for evidence instead of
guessing from the current file.

Typical assistant questions:

- "Who calls this function across indexed repos?"
- "What implements this interface?"
- "What workload uses this queue in prod?"
- "Trace this RDS instance back to Terraform and source code."
- "What changes if I modify this service?"
- "Compare stage and prod for this workload."

Start with [MCP Guide](guides/mcp-guide.md) and the
[MCP Cookbook](reference/mcp-cookbook.md).

## Runtime Profiles

Eshu exposes one query model, but different runtime profiles have different
backing truth:

| Profile | Best fit |
| --- | --- |
| Local binaries | Fast development against one workspace. Use this while changing Eshu or indexing a local checkout. |
| Docker Compose | Full local API/MCP/ingester/reducer stack with Postgres and NornicDB. Use this for product behavior and local graph truth. |
| Kubernetes | Shared team deployment with continuous indexing, collectors, telemetry, and operational controls. |

Lightweight local paths should return explicit unsupported-capability responses
when a graph-authoritative answer is not available. Full-stack and production
profiles should expose the authoritative surface after indexing and projection
complete.

## Open Source Model

Eshu is MIT licensed, self-hosted, and does not phone home. When observability
is enabled, telemetry goes to your configured OTLP and Prometheus targets.

Contributions should extend current contracts instead of adding parallel docs
or hidden behavior. New collectors, languages, query capabilities, and
deployment patterns need source facts, schema/versioning rules, tests, and
operator-visible proof.

## Start Here

- [Start Here](start-here.md) - choose a local, MCP, API, or Kubernetes path
- [Run Locally](run-locally/index.md) - local binaries and Docker Compose
- [MCP Guide](guides/mcp-guide.md) - connect Eshu to your AI assistant
- [HTTP API](reference/http-api.md) - automation and service-to-service access
- [Use Cases](use-cases.md) - workflow examples
- [Architecture](architecture.md) - runtime, storage, and graph model
