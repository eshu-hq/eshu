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
| `eshu-collector-package-registry` | `collector-package-registry/` | Long-running package-registry collector |
| `eshu-collector-aws-cloud` | `collector-aws-cloud/` | Long-running AWS cloud collector |
| `eshu-webhook-listener` | `webhook-listener/` | Long-running GitHub/GitLab webhook intake |
| `eshu-admin-status` | `admin-status/` | Admin/status read helper |
| `eshu-workflow-coordinator` | `workflow-coordinator/` | Long-running workflow coordinator |

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
  tfstate[collector-terraform-state] --> postgres
  aws[collector-aws-cloud] --> postgres
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

- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/reference/cli-reference.md`
- `docs/docs/reference/local-testing.md`
