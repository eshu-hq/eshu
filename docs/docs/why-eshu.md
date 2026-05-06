# Why Eshu

## The Problem

When you refactor a service, your AI assistant sees the code in front of it. It does not see the Terraform module that provisions the database, the ArgoCD application that deploys the workload, the Kubernetes manifest that configures replicas and secrets, or the three other services whose queue consumers break if you change the API contract.

That context exists. It is spread across your repositories, Helm charts, Terraform modules, ArgoCD apps, cloud consoles, and the heads of your senior engineers. But nothing connects it into a single queryable model — so engineers stitch it together by hand, every time, or skip the investigation and discover the blast radius in production.

Code search tools index code. IaC tools manage infrastructure. Service catalogs track ownership. None of them answer the question that actually matters during feature work, refactors, and incidents: **what connects to what, and what breaks if I change it?**

## What Eshu Does

Eshu builds a graph that connects source code, infrastructure definitions, and running workloads across your entire platform.

It indexes:

- **Source code** — functions, classes, imports, and call graphs across many languages (tree-sitter and native parsers)
- **Infrastructure-as-code** — Terraform/HCL, Kubernetes manifests, Helm charts, Kustomize overlays, CloudFormation
- **Deployment topology** — ArgoCD Applications and ApplicationSets, Crossplane XRDs and Claims
- **Cross-repo relationships** — module sources, image references, repo URLs, shared resources

The result is a graph you can query from the CLI, through MCP in your AI assistant, or via HTTP API. Same capabilities, same query model, three interfaces.

## Code Intelligence Query Surface

For software engineers, Eshu is not just "search plus infrastructure." It is a
code-intelligence surface designed to help AI assistants and humans trace
execution, understand structure, and scope change safely across large
codebases.

That means Eshu should answer code questions like:

- **Definitions and symbol lookup** — where a function, class, module, method,
  or variable is defined
- **Fuzzy code search** — exact-name, prefix, substring, and content search
  across indexed repositories
- **Structural code understanding** — methods on a class, inheritance trees,
  implementations, decorators, argument names, imports, and references
- **Execution tracing** — direct callers, direct callees, transitive callers,
  transitive callees, and full call-chain paths across files and repositories
- **Impact analysis** — what breaks if a function, service, or module changes
- **Code quality** — dead code detection, cyclomatic complexity, and hotspot
  discovery for the most complex functions

Typical code questions Eshu should support include:

- "Where is `process_payment` defined?"
- "Find the `User` class for me."
- "Show me any code related to database connection."
- "What other functions call `get_user_by_id`?"
- "Show me the full call chain from `main` to `process_data`."
- "Find all functions that directly or indirectly call `validate_input`."
- "What methods does the `Order` class have?"
- "Show me the inheritance hierarchy for `BaseController`."
- "Which files import the `requests` library?"
- "Find all implementations of the `render` method."
- "Is there any dead or unused code in this project?"
- "Find the 5 most complex functions in the codebase."

## What Makes Eshu Different

Unlike code-only search tools, Eshu treats infrastructure as a first-class citizen. Unlike service catalogs, it is built from your actual code and IaC — not manually maintained metadata. Unlike grepping across repos, it understands relationships: what deploys what, what provisions what, what consumes what, and what breaks when something changes.

Key capabilities no other open source tool combines:

- **`find_blast_radius`** — transitive dependency analysis across repos and infrastructure boundaries
- **`trace_deployment_chain`** — follow a service through controller/platform evidence, deployment-source repositories, and backing infrastructure
- **`trace_resource_to_code`** — trace a cloud resource back to the Terraform module and repository that owns it
- **`compare_environments`** — diff the dependency surface of a workload between prod and staging
- **`find_change_surface`** — see what is impacted before you merge
- **`explain_dependency_path`** — understand why two entities are connected, with evidence for each hop

## A Framework For Extensibility

Eshu is not a monolith. The query model, the storage layer, the collection
pipeline, and the runtime profiles are all defined by explicit contracts so
they can evolve independently.

### Backend-agnostic capability ports

Every HTTP handler, MCP tool, and CLI command reads data through narrow
interfaces such as `GraphQuery` and `ContentStore` — not through concrete
database drivers. NornicDB is the default graph adapter today. Neo4j remains
an explicit official alternative behind the same ports — details in
[ADR 2026-04-22](adrs/2026-04-22-nornicdb-graph-backend-candidate.md).
Capability ports are why swapping a backend is a wiring concern plus a
conformance-matrix run, not a handler rewrite. We explicitly rejected an
ORM as the central abstraction because graph traversal and transitive
semantics are not well represented by row-level ORMs. Details:
[Architecture — Capability Ports](architecture.md) and
[Capability Conformance Spec](reference/capability-conformance-spec.md).

This is a product advantage, not only an implementation detail. Eshu treats
NornicDB as the default first-class graph backend and Neo4j as a first-class
compatibility path for teams that already rely on Neo4j. Both backends must run
the same Eshu graph model through the shared Cypher/Bolt contract. That keeps the
platform portable without asking maintainers to own two separate graph-writing
systems.

Schema bootstrap follows the same adapter rule. Neo4j receives the shared
production DDL unchanged; NornicDB receives a narrow schema-dialect rendering
for compatibility gaps such as composite node identity. That route is
deliberately limited to DDL so reducers, handlers, CLI, HTTP, and MCP tools
do not fork into separate backend-specific codepaths.

### Conformance before "supported"

No backend is advertised as supported because it "speaks Cypher." A backend is
supported only after it passes the machine-readable capability matrix at
`specs/capability-matrix.v1.yaml` for the intended runtime profile. That matrix
lists every capability (exact symbol lookup, transitive callers, dead-code,
blast-radius, etc.) with per-profile status, max truth level, p95 latency
budget, and verification gates. See
[Capability Conformance Spec](reference/capability-conformance-spec.md).

### OCI-packaged collector plugins

Collectors observe source truth (git repos, Terraform state, Kubernetes
manifests, Helm, ArgoCD, Crossplane) and emit versioned facts. They do not
write graph truth directly — that stays owned by the reducer and canonical
graph writers. Adding a new collector family does not require patching the
core runtime. Plugins distribute as OCI artifacts with signed provenance
(Sigstore/Cosign), allowlist-based activation, and hard failure on incompatible
fact-schema versions. See
[Fact Envelope Reference](reference/fact-envelope-reference.md),
[Fact Schema Versioning](reference/fact-schema-versioning.md), and
[Plugin Trust Model](reference/plugin-trust-model.md).

### Language Query DSL

For structured semantic queries over indexed code — "list all decorators on
this class," "which files import this module," "show method signatures with
argument names" — Eshu exposes the `execute_language_query` MCP tool and the
matching `/api/v0/code/language-query` HTTP route. The query payload is a
small structured DSL that maps onto capability IDs such as
`symbol_graph.class_methods` and `symbol_graph.decorators`. See
[Language Query DSL Reference](reference/language-query-dsl.md).

## One Query Model, Multiple Truth Levels

Eshu exposes one query model through CLI, MCP, and HTTP API, but not every
runtime shape has the same backing truth available at all times.

The intended operating model is:

- **Lightweight local mode** should excel at code lookup and code comprehension:
  exact symbol lookup, fuzzy search, variable lookup, content search, decorator
  and argument-name search, import discovery, class-method listing,
  inheritance-aware structure where available, and complexity analysis
- **Authoritative graph mode** should add full execution and impact truth:
  direct callers and callees, transitive callers and callees, path tracing,
  dead-code detection, cross-repo blast radius, and code-plus-infrastructure
  dependency analysis
- **Full local stack and production** should expose the authoritative surface so
  engineers can ask the same high-value questions locally before they merge or
  in a deployed environment during incidents

The architecture contract is that Eshu surfaces a structured truth label across
CLI, MCP, and HTTP responses as this work lands. See
`2026-04-20-embedded-local-backends-desktop-mode.md`. See
`truth-label-protocol.md` for the wire contract and freshness semantics.

### Example: what the same question returns per profile

Example query: "who are the transitive callers of `process_payment`?"

| Profile | Capability: `call_graph.transitive_callers` | Response |
| --- | --- | --- |
| `local_lightweight` | `unsupported` | Structured `unsupported_capability` error — run against full stack or production. |
| `local_full_stack` | `exact` | Full transitive call graph from the reduced authoritative graph. |
| `production` | `exact` | Full transitive call graph across all indexed repos. |

Example query: "find `process_payment` in this repo."

| Profile | Capability: `code_search.exact_symbol` | Response |
| --- | --- | --- |
| `local_lightweight` | `exact` | Indexed-entity lookup from embedded Postgres. |
| `local_full_stack` | `exact` | Indexed-entity lookup from full-stack Postgres. |
| `production` | `exact` | Indexed-entity lookup from production Postgres. |

The full per-capability matrix lives in `specs/capability-matrix.v1.yaml` and
is mirrored in [Capability Conformance Spec](reference/capability-conformance-spec.md).

## Who It's For

### Software engineers

Before modifying a service, you need to understand callers, callees, dependencies, downstream consumers, and likely blast radius. Eshu answers these questions from the graph instead of requiring manual investigation across repos and IaC. `analyze_code_relationships` traces who calls what. `find_code` locates implementations across indexed repos. `find_dead_code` returns derived dead-code candidates after default entrypoint, Go public-API, test, and generated-code exclusions. `find_dead_iac` surfaces unused or ambiguous Terraform modules, Helm charts, Kustomize bases/overlays, Ansible roles, and Docker Compose services from indexed IaC reachability evidence. `find_most_complex_functions` surfaces complexity hotspots before they become incidents. `find_blast_radius` shows transitive impacts across repos and infrastructure boundaries.

### Platform / DevOps / SRE

Platform teams are constantly asked how workloads connect to infrastructure, what is shared, and what differs between environments. Eshu gives you — and the AI assistants your developers use — a real answer instead of tribal knowledge. `trace_deployment_chain` walks from controller/platform evidence through deployment-source repositories and backing infrastructure. `compare_environments` diffs the dependency surface between prod and staging. `find_infra_resources` and `analyze_infra_relationships` show what workloads share a database, queue, or secret — before someone changes it.

### Security & compliance

When you need to audit what services access a shared database, trace dependencies across repo boundaries, or understand infrastructure exposure, Eshu gives you evidence from the graph instead of asking around. `trace_resource_to_code` follows a cloud resource back to the Terraform module and repository that owns it. `find_infra_resources` shows what workloads touch a shared resource. `search_file_content` finds references across indexed repos. `analyze_infra_relationships` maps infrastructure dependencies for audit and review.

### Architects & tech leads

Large refactors and platform migrations need an ecosystem-level view — not a repo-at-a-time investigation. `get_ecosystem_overview` shows how repos connect across the org. `find_most_complex_functions` identifies complexity hotspots worth addressing. `find_change_surface` scopes the impact of a proposed change before it ships. `explain_dependency_path` answers "why are these two things connected?" with evidence for each hop.

### New engineers

Onboarding into a complex system usually means interrupting senior engineers for context about deployment topology, shared infrastructure, and cross-service dependencies. Eshu lets new team members query that context directly and get grounded answers from AI assistants on day one. `get_repo_context` and `get_service_context` provide structured overviews of what a repo or service does, what it depends on, and how it deploys.

## Why MCP Matters

MCP is how most engineers will experience Eshu.

Without MCP, you have a useful graph with a CLI and API. With MCP, that graph becomes available inside the AI workflow where engineers are already asking questions — Claude, Cursor, VS Code, or any MCP-compatible tool.

The difference: your AI assistant stops guessing from a single file and starts querying the actual dependency graph. It can answer "what breaks if I change this?" with evidence from the graph, not hallucinated assumptions from a partial code snapshot.

Examples of questions Eshu is designed to answer:

- "Who calls this function across all indexed repos?"
- "What implements this interface?"
- "Show me the most complex functions in this service"
- "What code is dead in this repo?"
- "What workload uses this queue in prod?"
- "Trace this RDS instance back to the Terraform module and the repos that reference it."
- "What changes if I modify this service?"
- "Compare stage and prod for this workload — what resources differ?"
- "Explain how this repo connects to the shared infrastructure platform."

## A Real Workflow

**Scenario:** In an authoritative full-stack or deployed environment, you need
to refactor the payment service's API contract.

1. **Scope the change** — use the MCP tool `find_blast_radius` for `payment-service` to see downstream repos, shared Terraform modules, and Crossplane claims.
2. **Understand the deployment** — use `trace_deployment_chain` to follow controller/platform evidence, deployment-source repositories, and backing resources.
3. **Check environment differences** — use `compare_environments` to inspect drift between prod and staging.
4. **Trace a shared resource** — use `trace_resource_to_code` to follow an RDS instance back to its Terraform module and consuming repositories.
5. **Ship with confidence** — you know the blast radius before you open the PR, not after the page.

## Open Source

Eshu is MIT licensed, self-hosted, and does not phone home. The
authoritative graph path now defaults to NornicDB, with Neo4j kept as the
official alternative for operators who need that path. Lightweight
local mode uses embedded Postgres and relational code-intelligence tables when
you do not need graph-authoritative reads. NornicDB has full-corpus
latest-main evidence; the remaining decision is whether Neo4j can meet the same
shared Cypher contract fast enough to stay more than compatibility-only, and
when NornicDB becomes release-backed instead of latest-main-backed. Language
parsing is owned by native Go packages backed by
tree-sitter, HCL, YAML/JSON, SCIP, and schema-aware extractors. Add parser
capability by extending the Go parser or relationship packages with fixtures
and focused tests.

Contributions welcome: new language parsers, IaC formats, query capabilities, and deployment patterns.

## Start Here

- [Quickstart](getting-started/quickstart.md) — index a repo and run your first query
- [MCP Guide](guides/mcp-guide.md) — connect Eshu to your AI assistant
- [HTTP API](reference/http-api.md) — automation and service-to-service access
- [Use Cases](use-cases.md) — detailed workflow examples
- [Architecture](architecture.md) — how Eshu works under the hood
