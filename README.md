# Eshu

**A self-hosted context graph that connects your code, dependencies, supply
chain, infrastructure, and runtime into one queryable, evidence-backed source of
truth — for engineers and the AI assistants working beside them.**

<p align="center">
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/eshu-hq/eshu?style=flat-square" alt="License">
  </a>
  <a href="https://github.com/eshu-hq/eshu/actions/workflows/test.yml">
    <img src="https://github.com/eshu-hq/eshu/actions/workflows/test.yml/badge.svg" alt="Tests">
  </a>
  <a href="docs/public/reference/code-coverage.md">
    <img src="https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Feshu-hq%2Feshu%2Fmain%2Fdocs%2Fpublic%2Freference%2Fcode-coverage-shield.json" alt="Go code coverage">
  </a>
  <a href="docs/public/index.md">
    <img src="https://img.shields.io/badge/docs-MkDocs-blue?style=flat-square" alt="Docs">
  </a>
  <img src="https://img.shields.io/badge/MCP-Compatible-green?style=flat-square" alt="MCP Compatible">
  <img src="https://img.shields.io/badge/go-1.26%2B-00ADD8?style=flat-square&logo=go" alt="Go 1.26+">
  <img src="https://img.shields.io/badge/ghcr.io-image-2496ED?style=flat-square&logo=docker&logoColor=white" alt="GHCR Image">
  <img src="https://img.shields.io/badge/helm-OCI-0F1689?style=flat-square&logo=helm&logoColor=white" alt="Helm OCI Chart">
</p>

Eshu indexes source code, dependencies, container images, infrastructure,
deployment config, runtime topology, external collector facts, and documentation
into one queryable graph. It is the durable institutional-knowledge layer your
team and its assistants query instead of re-deriving the answer from five
repositories, three dashboards, a Helm chart, and a Terraform module.

The launch beachhead is **supply-chain traceability**: a single trace from a
vulnerable dependency through the container images that ship it, the workloads
that run them, and the source and Terraform that own them — every hop backed by
evidence.

Use Eshu to ask:

- "Which deployed images contain this vulnerable package, and what source and
  Terraform own them?"
- "Who calls this function across all indexed repos?"
- "What deploys this service to production?"
- "Which workloads share this database, queue, or secret?"
- "What breaks if I change this Terraform module?"
- "Show the evidence behind this service-to-infrastructure link."

## What Eshu Can Do

These map to the capability families reported by the `get_capability_catalog`
MCP tool and the public tool families in
[MCP Reference](docs/public/reference/mcp-reference.md).

| Capability area | What you get |
| --- | --- |
| Code intelligence | Find symbols, callers, call chains, imports, inheritance, complexity, and dead-code candidates across 20+ languages. <!-- capability-state: id=code_search.symbol_lookup state=general_availability --> <!-- product-claim: id=readme.code-intelligence.language-breadth --> |
| Code-to-cloud tracing | Connect source repos, container images, Kubernetes workloads, Helm/Kustomize/ArgoCD config, Terraform resources, and cloud observations with evidence at each hop. <!-- capability-state: id=code_to_cloud.trace_exposure_path state=general_availability --> <!-- product-claim: id=readme.code-to-cloud.evidence-continuity --> |
| Supply-chain traceability | Track dependencies across 13 package ecosystems, reconcile 4 advisory sources (CISA KEV, FIRST EPSS, OSV, NVD), and follow SBOM-to-image attachments with subject-digest evidence. These reads are served by external collectors (package registry, container registry, CI/CD, SBOM/attestation, advisory feeds, provider security alerts) that are off in a default deploy: enable them with `ESHU_COLLECTOR_INSTANCES_JSON` plus provider credentials, otherwise the `list_*` supply-chain tools return well-formed empty pages. <!-- capability-state: id=supply_chain.impact_findings.list state=gated --> <!-- product-claim: id=readme.supply-chain.default-gated --> |
| Vulnerability reachability | Go reachability via govulncheck <!-- capability-state: id=reachability.go.govulncheck state=general_availability -->; value-flow reachability for Python, TypeScript, and JavaScript <!-- capability-state: id=reachability.python.value_flow state=gated --> <!-- capability-state: id=reachability.typescript.value_flow state=gated --> <!-- capability-state: id=reachability.javascript.value_flow state=gated -->; bounded JVM reachability <!-- capability-state: id=reachability.jvm.bounded state=preview -->. <!-- product-claim: id=readme.reachability.summary state=unguarded --> |
| Security and IAM posture | Surface hardcoded secrets, secret-access paths, and IAM trust and privilege chains with the evidence behind each finding. <!-- capability-state: id=security.hardcoded_secrets state=general_availability --> <!-- capability-state: id=secrets_iam.secret_access_paths.list state=general_availability --> <!-- product-claim: id=readme.security-iam.evidence-backed-findings --> |
| Change-risk analysis | Ask for blast radius, shared dependencies, change surface, and direct versus transitive relationships before a change lands. <!-- capability-state: id=platform_impact.blast_radius state=general_availability --> <!-- product-claim: id=readme.change-risk.blast-radius --> |
| IaC and re-platforming | Find unmanaged resources, propose Terraform import plans, and compose multi-cloud re-platforming plans. <!-- capability-state: id=replatforming.plan.readiness state=general_availability --> <!-- product-claim: id=readme.replatforming.plan-readiness --> |
| AI assistant context | Serve indexed truth through 161 read-only MCP tools so Codex, Claude, Cursor, VS Code, and other clients answer with evidence instead of guessing. <!-- capability-state: id=capability_catalog.list state=general_availability --> <!-- product-claim: id=readme.mcp-tool-count.indexed-truth --> |
| Operations visibility | Track ingestion, reducer queues, graph writes, runtime drift, freshness, health, metrics, traces, and logs. <!-- capability-state: id=freshness.changed_since state=general_availability --> <!-- product-claim: id=readme.operations-visibility.freshness --> |
| Extensibility | Add parsers, collectors, package-registry sources, Terraform providers, and language support with fixture-backed tests. |

The Go code coverage badge links to an advisory package report. It is one signal, not a replacement for replay, golden-corpus, or full-corpus proof.

## Pick Your First Path

| I want to... | Start here |
| --- | --- |
| Get one successful local run and first answer | [First successful run](docs/public/getting-started/first-successful-run.md) |
| Try the full API and MCP service stack on my laptop | [Docker Compose](docs/public/run-locally/docker-compose.md) |
| Develop Eshu or run one local workspace service | [Local binaries](docs/public/run-locally/local-binaries.md) |
| Connect Codex, Claude, Cursor, or VS Code | [Connect MCP](docs/public/mcp/index.md) |
| Deploy Eshu as a shared team service | [Kubernetes deployment](docs/public/deploy/kubernetes/index.md) |
| Deploy on EKS | [EKS deployment](docs/public/deploy/eks/index.md) |
| Monitor, debug, or tune a deployment | [Operate Eshu](docs/public/operate/index.md) |
| Code on Eshu with an AI agent | [Code with agents](docs/public/guides/coding-with-agents.md) |
| Understand the architecture | [How Eshu works](docs/public/concepts/how-it-works.md) |
| Contribute | [Contributing](CONTRIBUTING.md) |

## Interfaces

- **CLI:** local setup, indexing, analysis, and operator commands.
- **MCP:** assistant-facing tools for Codex, Claude, Cursor, VS Code, and other
  MCP clients.
- **HTTP API:** automation and platform integration.
- **Helm:** split-service Kubernetes deployment for shared team use.

## Documentation

If you only read three pages first, read these:

1. [First Successful Run](docs/public/getting-started/first-successful-run.md)
2. [Start Here](docs/public/start-here.md)
3. [Connect MCP](docs/public/mcp/index.md)

The full documentation is organized by job:

- [Use Eshu](docs/public/use/index.md): index repositories and ask code or
  infrastructure questions.
- [Deploy Eshu](docs/public/deploy/kubernetes/index.md): install the shared
  service with Helm or follow the EKS runbook.
- [Operate Eshu](docs/public/operate/index.md): health checks, telemetry, and
  troubleshooting.
- [Understand Eshu](docs/public/understand/index.md): architecture, graph model,
  and runtime modes.
- [Extend Eshu](docs/public/extend/index.md): collectors, components, language
  support, and plugin contracts.
- [Code with agents](docs/public/guides/coding-with-agents.md): repo rules,
  required proof, and the safe workflow for AI-assisted changes.
- [Reference](docs/public/reference/cli-reference.md): CLI, API, MCP,
  configuration, telemetry, and backend details.

## Maturity At A Glance

| Area | Status |
| --- | --- |
| Local Docker Compose stack | Supported |
| Local Eshu service from checkout | Supported for development and workspace-local use |
| MCP server | Supported |
| Kubernetes deployment | Supported through Helm |
| NornicDB graph backend | Default supported backend |
| Neo4j graph backend | Supported compatibility backend |
| Optional collectors | Varies by collector; see [Collector And Reducer Readiness](docs/public/reference/collector-reducer-readiness.md) |

## Repository Surfaces

| Path | Purpose |
| --- | --- |
| [go/](go/) | Go module for the CLI, API, MCP server, ingester, reducer, collectors, storage, parser, and query code. |
| [deploy/helm/eshu/](deploy/helm/eshu/) | Public split-service Helm chart. |
| [deploy/observability/](deploy/observability/) | Prometheus alert rules and local OpenTelemetry Collector config. |
| [apps/console/](apps/console/) | Private read-only product console for Eshu graph data. |
| [docs/public/](docs/public/) | MkDocs source for the public documentation site. |

## Project

- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Testing guide](TESTING.md)
- [Developer guide](DEVELOPING.md)
- [License](LICENSE)

Eshu is self-hosted and does not require outbound vendor telemetry. When
observability is enabled, it uses your configured OTLP and Prometheus targets.
