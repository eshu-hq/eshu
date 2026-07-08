# Commands

Each subdirectory builds one Eshu executable. This directory is a navigation
root, not a Go package — each child has its own rich `README.md` and
`AGENTS.md`.

The public CLI command is `eshu`. The service binaries use ESHU-prefixed names
when installed for local runtime work, such as `eshu-api`, `eshu-mcp-server`,
`eshu-ingester`, `eshu-reducer`, and `eshu-webhook-listener`. Use
`scripts/install-local-binaries.sh` from the repository root when you need that
exact binary set on `PATH`.

## Binary-to-runtime map

| Binary | Subdirectory | Lifecycle |
| --- | --- | --- |
| `eshu` (CLI) | `eshu/` | One-shot CLI commands plus subcommand dispatch |
| `eshu-api` | `api/` | Long-running HTTP API |
| `eshu-mcp-server` | `mcp-server/` | Long-running MCP tool server |
| `eshu-ingester` | `ingester/` | Long-running git sync + parse + fact emission |
| `eshu-projector` | `projector/` | Long-running source-local projection (local profiles) |
| `eshu-reducer` | `reducer/` | Long-running cross-domain materialization |
| `eshu-bootstrap-index` | `bootstrap-index/` | One-shot multi-phase orchestration |
| `eshu-bootstrap-data-plane` | `bootstrap-data-plane/` | One-shot data-plane setup |
| `eshu-collector-git` | `collector-git/` | Local git-collector helper |
| `eshu-collector-confluence` | `collector-confluence/` | Long-running Confluence documentation collector |
| `eshu-collector-terraform-state` | `collector-terraform-state/` | Long-running Terraform-state collector |
| `eshu-collector-component-extension` | `collector-component-extension/` | Long-running process-backed component extension collector |
| `eshu-collector-package-registry` | `collector-package-registry/` | Long-running package-registry collector |
| `eshu-collector-sbom-attestation` | `collector-sbom-attestation/` | Long-running hosted SBOM and attestation collector |
| `eshu-collector-security-alerts` | `collector-security-alerts/` | Long-running hosted provider security-alert collector |
| `eshu-collector-cicd-run` | `collector-cicd-run/` | Long-running hosted GitHub Actions CI/CD run collector |
| `eshu-collector-pagerduty` | `collector-pagerduty/` | Long-running PagerDuty incident-context collector |
| `eshu-collector-jira` | `collector-jira/` | Long-running Jira work-item evidence collector |
| `eshu-collector-grafana` | `collector-grafana/` | Long-running live Grafana metadata collector |
| `eshu-collector-prometheus-mimir` | `collector-prometheus-mimir/` | Long-running live Prometheus and Mimir metadata collector |
| `eshu-collector-loki` | `collector-loki/` | Long-running live Loki log-signal metadata collector |
| `eshu-collector-tempo` | `collector-tempo/` | Long-running live Tempo trace-signal metadata collector |
| `eshu-scanner-worker` | `scanner-worker/` | Long-running isolated security analyzer worker |
| `eshu-collector-aws-cloud` | `collector-aws-cloud/` | Long-running AWS cloud collector |
| `eshu-collector-gcp-cloud` | `collector-gcp-cloud/` | Long-running GCP Cloud Asset Inventory collector |
| `eshu-webhook-listener` | `webhook-listener/` | Long-running GitHub/GitLab webhook intake |
| `eshu-admin-status` | `admin-status/` | Admin/status read helper |
| `eshu-workflow-coordinator` | `workflow-coordinator/` | Long-running workflow coordinator |
| `capability-inventory` | `capability-inventory/` | Generate/verify the capability catalog artifact (dev/CI tool) |
| `audit-preflight` | `audit-preflight/` | Validate competitive-audit issues against the preflight contract (dev/CI tool) |
| `fact-envelope-adapter` | `fact-envelope-adapter/` | Generate/verify the shared fact-envelope adapter artifact (dev/CI tool) |

## Pipeline shape

```mermaid
flowchart LR
  ingester[ingester] --> postgres[(postgres facts/queue)]
  projector[projector] -.local profiles.-> postgres
  postgres --> reducer[reducer]
  reducer --> graph[(graph backend)]
  bootstrap[bootstrap-index] -.one-shot.-> ingester
  bootstrap -.one-shot.-> reducer
  workflow[workflow-coordinator] --> postgres
  component[collector-component-extension] --> postgres
  tfstate[collector-terraform-state] --> postgres
  sbom[collector-sbom-attestation] --> postgres
  alerts[collector-security-alerts] --> postgres
  cicd[collector-cicd-run] --> postgres
  pagerduty[collector-pagerduty] --> postgres
  jira[collector-jira] --> postgres
  grafana[collector-grafana] --> postgres
  metrics[collector-prometheus-mimir] --> postgres
  loki[collector-loki] --> postgres
  tempo[collector-tempo] --> postgres
  aws[collector-aws-cloud] --> postgres
  gcp[collector-gcp-cloud] --> postgres
  scanner[scanner-worker] --> postgres
  webhook[webhook-listener] --> postgres
  api[api] --> graph
  api --> postgres
  mcp[mcp-server] --> api
```

For the full lifecycle of any one binary, open its `README.md` and
`AGENTS.md`.

## Per-package documentation convention

Every Go package directory under `go/cmd/` carries three files:

- `doc.go` — godoc contract.
- `README.md` — architectural and operational lens with mermaid flow
  diagrams and runbook-shape operational notes.
- `AGENTS.md` — guidance for LLM assistants editing the binary.

## Dependencies

Each `cmd/` subdirectory wires together internal packages into a binary;
see the per-binary `main.go` for the exact set. Shared process wiring
lives in `internal/runtime` and `internal/app`.

## Telemetry

Process-level telemetry bootstrap (service namespace, OTEL exporter, log
sinks) is configured by `internal/runtime` and `internal/telemetry`. Each
binary inherits that contract; packages do not register their own meter
providers.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/cli-reference.md`
- `docs/public/reference/local-testing.md`
