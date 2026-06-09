# Local Testing Reference

This page is the verification map for engineers and agents changing Eshu. For
first-time setup, use [Run Locally](../run-locally/index.md). For operator
checks, use [Operate Eshu](../operate/index.md) and
[Health Checks](../operate/health-checks.md).

Use the smallest gate that proves the touched behavior, then run the hygiene
checks required by the files you changed. Do not call work ready without citing
the commands you actually ran.

## Common Compose Environment

When running commands directly against the default local Compose stack:

```bash
export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI=bolt://localhost:7687
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=change-me
export DEFAULT_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export ESHU_CONTENT_STORE_DSN=postgresql://eshu:change-me@localhost:15432/eshu
export ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu
```

For `docker-compose.neo4j.yml`, use `ESHU_GRAPH_BACKEND=neo4j` and database
`neo4j` instead.

## What To Run

| Change area | Use this page |
| --- | --- |
| Onboarding first-answer dogfood proof | [First five minutes benchmark](local-testing/first-five-minutes-benchmark.md) |
| Cross-surface answer-quality dogfood proof | [Answer Quality Scorecard](local-testing/answer-quality-scorecard.md) |
| Remote all-collector Compose proof | [Remote collector E2E](local-testing/remote-collector-e2e.md) |
| Confluence, Jira, vulnerability source, and live registry smokes | [Collector live smokes](local-testing/collector-live-smokes.md) |
| Normal package, Compose, graph, Terraform-state, webhook, and docs gates | [Verification gates](local-testing/verification-gates.md) |
| Discovery report loop for noisy repositories | [Discovery advisory playbook](local-testing/discovery-advisory.md) |
| Worker knobs, pprof, and phase CPU profile capture | [Profiling and concurrency](local-testing/profiling-and-concurrency.md) |

## Quick Verification Matrix

| If you touched | Minimum verification |
| --- | --- |
| Answer-quality scorecard criteria, CLI, or docs | `cd go && go test ./internal/answerquality -count=1`, `cd go && go test ./cmd/eshu -run 'TestAnswerQualityScorecardCommand' -count=1`, and the docs build |
| Docs, `CLAUDE.md`, `AGENTS.md`, or README files | `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` |
| GitHub workflow or CodeQL setup guidance | `scripts/test-verify-codeql-setup.sh` and `scripts/verify-codeql-setup.sh` |
| CLI/runtime wiring | `cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server -count=1` |
| Status/admin or completeness contract | `cd go && go test ./internal/status ./internal/query ./cmd/api -count=1` and `cd go && go vet ./internal/status ./internal/query ./cmd/api` |
| Replatforming plan, ownership-packet, or rollup API/MCP surface | `cd go && go test ./internal/mcp -run TestReplatforming -count=1` (see [Verification gates → Replatforming API/MCP parity proof](local-testing/verification-gates.md#replatforming-apimcp-parity-proof)) |
| Parser platform or collector snapshot flow | `cd go && go test ./internal/parser ./internal/collector/discovery ./internal/collector -count=1` |
| Terraform provider-schema evidence or relationship extraction | `cd go && go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1` |
| Parser, language-query, dead-code maturity, or relationship contribution docs | `scripts/verify-parser-relationship-kit.sh` plus the focused parser, query, relationship, or docs gate for the touched surface. |
| Compose, Helm, or deployable runtime shape | `cd go && go test ./cmd/api ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1` and `helm lint deploy/helm/eshu` |
| Facts-first indexing, queue, or resolution flow | `cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1` |
| Recovery, replay, or repair controls | `cd go && go test ./internal/recovery ./internal/runtime ./internal/status -count=1` |
| Hot-path Cypher, graph writes, queues, workers, leases, batching, or runtime knobs | `scripts/test-verify-performance-evidence.sh` and `scripts/verify-performance-evidence.sh` |
| New collector family, provider, scanner, or hosted collector runtime | `scripts/test-verify-collector-authoring-gate.sh` and `scripts/verify-collector-authoring-gate.sh` |
| New or changed Go package under `go/internal` or `go/cmd` | `scripts/test-verify-package-docs.sh` and `scripts/verify-package-docs.sh` |
| Go source, comments, package contracts, or generated docs | `cd go && golangci-lint run ./...` |
| Repo hygiene gates | `git diff --check` |

## Remote Collector E2E Compose Proof

Use [Remote collector E2E](local-testing/remote-collector-e2e.md) when changing
`docker-compose.remote-e2e.yaml` or hosted collector recovery.

Before accepting a remote collector E2E run, also run the hosted runtime-state
gate in [Remote E2E Runtime State](remote-e2e-runtime-state.md). It verifies
the API, MCP server, ingester, resolution engine, workflow coordinator, hosted
collectors, and checkpointed queue-zero signal.

## Discovery Advisory Playbook

Use [Discovery advisory](local-testing/discovery-advisory.md) when a repository
is slow, unexpectedly large, or timeout-heavy. This is diagnostic evidence, not
a stable API contract.

## Process Profiling

Use [Profiling and concurrency](local-testing/profiling-and-concurrency.md)
for `ESHU_PPROF_ADDR`, concurrency knobs, and phase CPU capture.

## Docs And Hygiene

Docs, `CLAUDE.md`, `AGENTS.md`, and README changes require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
