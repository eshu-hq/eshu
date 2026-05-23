# Commands

Each subdirectory builds one Eshu executable. This directory is a navigation
root, not a Go package. Each child owns its operational contract in `README.md`;
assistant workflow rules stay in the repository root `AGENTS.md` unless a
binary needs local rules that do not belong in the public command README.

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

Long-running binaries either observe source systems and commit facts, drain
durable work into graph/content state, coordinate claimable collector work, or
serve bounded read surfaces. One-shot binaries initialize schema or bootstrap an
empty environment. For the lifecycle of any one binary, open its `README.md`.

## Per-package documentation convention

Every Go command directory under `go/cmd/` carries at least two package docs:

- `doc.go` - godoc contract.
- `README.md` - architectural and operational lens with runbook-shape notes.

Many command directories also carry package-local `AGENTS.md` files when the
binary has scoped workflow rules. Do not delete those files unless the root
agent guide explicitly replaces the same scope and precedence.

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
