# Source Layout

Eshu is organized around runtime and domain packages.
Fixture inputs under `tests/fixtures/` exist to exercise parser behavior and
ecosystem relationships.

Pair this page with the [Collector Authoring Guide](../guides/collector-authoring.md).
That guide explains boundary rules; this page explains where those boundaries
live in the repository today.

## Top-Level Map

| Path | Responsibility |
| :--- | :--- |
| `go/cmd/` | buildable binaries for API, MCP, CLI, ingester, reducer, bootstrap, and local verification runtimes |
| `go/internal/app/` | runtime composition, configuration, and shared service wiring |
| `go/internal/backendconformance/` | graph-backend conformance matrix parsing plus reusable `GraphQuery` and Cypher write corpora |
| `go/internal/collector/` | Git collection, discovery, snapshotting, and fact shaping |
| `go/internal/content/` | content shaping and content-store persistence |
| `go/internal/coordinator/` | workflow coordinator service ordering, planner interfaces, durable admission, retry, and telemetry ownership |
| `go/internal/coordinator/cicdrun/` | CI/CD run scheduler request validation and deterministic workflow planning |
| `go/internal/coordinator/componentactivation/` | dependency-neutral generic component-activation configuration parsing and validation shared by the component-extension planner and unrelated root scheduling/audit files |
| `go/internal/coordinator/componentextensionplanner/` | generic component-extension scheduler activation-scoped workflow planning |
| `go/internal/coordinator/gcpplanner/` | GCP Cloud Asset Inventory scheduler scope configuration parsing, validation, and deterministic workflow planning |
| `go/internal/coordinator/grafanaplanner/` | Grafana scheduler request validation, target filtering, privacy, and deterministic workflow planning |
| `go/internal/coordinator/jiraplanner/` | Jira scheduler request and target validation, webhook-scope membership, privacy, and deterministic workflow planning |
| `go/internal/coordinator/lokiplanner/` | Grafana Loki scheduler request validation, target filtering, and deterministic workflow planning |
| `go/internal/coordinator/pagerdutyplanner/` | PagerDuty scheduler request and target validation, webhook-scope membership, privacy, and deterministic workflow planning |
| `go/internal/coordinator/plannercontract/` | dependency-neutral scheduler plan-key validation |
| `go/internal/coordinator/prometheusmimir/` | Prometheus/Mimir scheduler request validation, target filtering, privacy, and deterministic workflow planning |
| `go/internal/coordinator/scannerworker/` | scanner-worker scheduler configuration validation, requested-scope privacy, and deterministic workflow planning |
| `go/internal/coordinator/securityalert/` | provider security-alert scheduler request validation and deterministic workflow planning |
| `go/internal/coordinator/sbomattestation/` | hosted SBOM and attestation scheduler request validation and deterministic workflow planning |
| `go/internal/coordinator/tempoplanner/` | Grafana Tempo scheduler request validation, target filtering, and deterministic workflow planning |
| `go/internal/coordinator/vaultlive/` | Vault metadata scheduler request validation and deterministic workflow planning |
| `go/internal/facts/` | durable fact models and queue contracts |
| `go/internal/graph/` | canonical graph schema and write helpers |
| `go/internal/mcp/` | MCP ordered assembly, global route fanout and adapters, dispatch, authorization, transport, timeouts, response budgets, envelopes, and telemetry |
| `go/internal/mcp/admissiondecisions/` | admission-decisions MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/ask/` | Ask Eshu MCP registration plus pure family membership and dependency-neutral route selection |
| `go/internal/mcp/cicd/` | CI/CD run-correlation MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/cloud/` | cloud inventory and runtime-drift MCP tool registration definitions |
| `go/internal/mcp/codeowners/` | CODEOWNERS ownership MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/containerimage/` | container-image identity, tag-history, and aggregate MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/documentation/` | documentation-family MCP tool registration definitions |
| `go/internal/mcp/ecosystem/` | ecosystem, repository-context, infrastructure-impact, and change-planning MCP tool registration definitions |
| `go/internal/mcp/freshness/` | generation, repository, and service freshness MCP tool registration definitions |
| `go/internal/mcp/investigation/` | investigation workflow and evidence-packet MCP tool registration definitions |
| `go/internal/mcp/kubernetes/` | Kubernetes-correlation MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/observabilitycoverage/` | observability-coverage MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/packageregistry/` | package-registry MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/playbooks/` | query-playbook MCP tool registration definitions |
| `go/internal/mcp/relationships/` | code-relationship and relationship-edge MCP registrations plus pure family membership and dependency-neutral route selection |
| `go/internal/mcp/routecontract/` | dependency-neutral MCP route arguments and internal-request shape |
| `go/internal/mcp/secretsiam/` | secrets/IAM posture MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/securityalert/` | security-alert reconciliation listing, count, and inventory MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/semantic/` | semantic-evidence and semantic-search MCP tool registration definitions |
| `go/internal/mcp/service/` | service catalog, context, investigation, and intelligence-report MCP tool registration definitions |
| `go/internal/mcp/supplychainimpact/` | supply-chain-impact findings, count, inventory, and explanation MCP family membership and dependency-neutral route selection |
| `go/internal/mcp/toolcontract/` | dependency-neutral MCP tool registration shape |
| `go/internal/mcp/visualization/` | visualization-packet MCP registration plus pure family membership and dependency-neutral route selection |
| `go/internal/parser/` | native parser registry, language adapters, and SCIP support |
| `go/internal/projector/` | source-local projection stages and failure classification |
| `go/internal/projector/awsrelationship/` | AWS relationship-edge reducer-intent family builder |
| `go/internal/projector/azure/` | Azure resource and relationship reducer-intent family builders |
| `go/internal/projector/ec2/` | EC2 instance-posture reducer-intent family builders |
| `go/internal/projector/gcp/` | GCP resource and relationship reducer-intent family builders |
| `go/internal/projector/iamcanassume/` | IAM CAN_ASSUME trust-edge reducer-intent family builder and its aws_iam_permission decode wrapper |
| `go/internal/projector/incidentrouting/` | PagerDuty incident-routing reducer-intent family builder |
| `go/internal/projector/intent/` | dependency-neutral reducer-intent values, source labels, and immutable fact index for extracted projector families |
| `go/internal/projector/kubernetes/` | Kubernetes live-workload and namespace reducer-intent family builders |
| `go/internal/projector/packagesource/` | package-source-correlation reducer-intent family builder |
| `go/internal/projector/rds/` | RDS posture-materialization reducer-intent family builder |
| `go/internal/projector/s3/` | S3 LOGS_TO, external-principal-grant, and internet-exposure reducer-intent family builders |
| `go/internal/projector/security/` | security-alert reconciliation and AWS security-group reducer-intent family builders |
| `go/internal/projector/servicecatalog/` | service-catalog-correlation reducer-intent family builder |
| `go/internal/projector/workloadcloud/` | workload-cloud-relationship reducer-intent family builder |
| `go/internal/query/` | HTTP query/admin handlers plus OpenAPI support |
| `go/internal/query/querycontract/` | dependency-neutral query profiles, envelopes, capability registry, and read ports |
| `go/internal/recovery/` | replay and repair domain logic |
| `go/internal/reducer/` | cross-domain reduction and shared projection ownership |
| `go/internal/reducer/contract/` | dependency-neutral reducer domain, intent, result, and handler contracts |
| `go/internal/relationships/` | infrastructure/deployment evidence extraction and resolution |
| `go/internal/runtime/` | probes, admin/status surfaces, retry policy, lifecycle hooks |
| `go/internal/scope/` | repository scope and generation identities |
| `go/internal/status/` | pipeline lifecycle and request reporting |
| `go/internal/storage/` | Postgres adapters plus backend-neutral Cypher graph writers and backend-specific graph adapters |
| `go/internal/telemetry/` | OTEL tracing, metrics, and structured logging |
| `go/internal/terraformschema/` | packaged Terraform provider schemas and schema loader |
| `go/internal/truth/` | canonical truth contracts |
| `deploy/` | deployment assets: Helm chart, minimal Kubernetes manifests, Argo CD examples, and local observability add-ons |
| `docs/` | operator docs, architecture, workflows, runtime references, and language references |
| `sdk/` | public extension SDK modules that stay independent from `go/internal` packages |
| `tests/fixtures/` | parser and ecosystem fixture corpora only |

## Runtime Binaries

The service boundary is explicit in `go/cmd/`:

- `api/`: HTTP API binary
- `mcp-server/`: MCP server binary
- `eshu/`: top-level CLI
- `bootstrap-index/`: one-shot indexing seed
- `collector-git/`: local collector verification runtime
- `collector-aws-cloud/`: claim-driven AWS cloud collector runtime
- `collector-terraform-state/`: claim-driven Terraform-state collector runtime
- `ingester/`: deployed ingestion runtime
- `projector/`: local projector verification runtime
- `reducer/`: deployed reduction and repair runtime
- `admin-status/`: local status renderer

The normal runtime contract is implemented through these binaries. Do not
reintroduce service logic in an alternate shell, bridge process, or secondary
runtime tree outside these binaries.

## Collector, Parser, And Projection Ownership

The write path is intentionally split:

- `go/internal/collector/` owns Git source acquisition, repository selection,
  discovery, snapshotting, and fact emission
- `go/internal/parser/` owns parser registration, per-file parse execution,
  fixture-matrix language semantics, and SCIP support
- `go/internal/projector/` owns source-local projection stages for entities,
  files, relationships, and workloads
- `go/internal/reducer/` owns cross-domain materialization, shared projection
  intents, platform materialization, dependency projection, and repair flows
- `go/internal/relationships/` owns relationship extraction, including
  Terraform schema-driven evidence backed by the packaged schemas in
  `go/internal/terraformschema/`

This is the normal-path ownership. If a new collector or parser feature is
added, it belongs under these Go packages.

## Query And Admin Ownership

Read and operator surfaces live under:

- `go/internal/query/`: HTTP handlers, root compatibility aliases, and OpenAPI
- `go/internal/query/querycontract/`: response contracts, profile gates, and read ports
- `go/internal/mcp/`: MCP ordered assembly, global route fanout and adapters,
  dispatch, authorization, transport, timeouts, response budgets, envelopes,
  and telemetry
- `go/internal/mcp/admissiondecisions/`: admission-decisions family
  membership and pure dependency-neutral request selection
- `go/internal/mcp/ask/`: Ask registration, family membership, and pure
  dependency-neutral request selection
- `go/internal/mcp/cicd/`: CI/CD run-correlation family membership and pure
  dependency-neutral request selection
- `go/internal/mcp/codeowners/`: CODEOWNERS ownership family membership and
  pure dependency-neutral request selection
- `go/internal/mcp/containerimage/`: container-image identity family membership
  and pure dependency-neutral request selection
- `go/internal/mcp/kubernetes/`: Kubernetes-correlation family membership and
  pure dependency-neutral request selection
- `go/internal/mcp/observabilitycoverage/`: observability-coverage family
  membership and pure dependency-neutral request selection
- `go/internal/mcp/packageregistry/`: package-registry family membership and
  pure dependency-neutral request selection
- `go/internal/mcp/relationships/`: relationship-family registrations, family
  membership decisions, and pure dependency-neutral request selection
- `go/internal/mcp/secretsiam/`: secrets/IAM posture family membership and
  pure dependency-neutral request selection
- `go/internal/mcp/securityalert/`: security-alert reconciliation family
  membership and pure dependency-neutral request selection
- `go/internal/mcp/supplychainimpact/`: supply-chain-impact family membership
  and pure dependency-neutral request selection
- `go/internal/mcp/visualization/`: visualization registration, family
  membership, and pure dependency-neutral request selection
- `go/internal/runtime/`: `/healthz`, `/readyz`, `/metrics`, `/admin/status`,
  retry policy, runtime lifecycle
- `go/internal/status/`: request lifecycle and indexing completeness reporting

The CLI delegates to the Go binaries and HTTP/query surfaces rather than
embedding a second service stack.

## Storage Ownership

All durable state is accessed through Go storage adapters:

- `go/internal/storage/postgres/`: facts, queues, content store, recovery,
  decisions, status, and lifecycle metadata
- `go/internal/storage/cypher/`: backend-neutral Cypher write contracts,
  canonical graph writers, edge helpers, retry wrappers, and write telemetry
- `go/internal/storage/neo4j/`: Neo4j-specific graph storage adapters
- `go/internal/content/`: content shaping and persistence helpers layered over
  the Postgres store

## Terraform Provider Schemas

Terraform provider schema assets are first-class runtime inputs, not leftover
artifacts:

- packaged assets live in `go/internal/terraformschema/schemas/*.json.gz`
- the loader and classification logic live in `go/internal/terraformschema/`
- runtime relationship extraction uses those assets through
  `go/internal/relationships/`

If provider schemas move or change format, update both the runtime code and the
operator docs so the dependency remains explicit.

If another note disagrees with this page, treat this page and the published
runtime/workflow docs as the current architecture.
