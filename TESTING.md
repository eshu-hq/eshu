# Testing

Eshu now validates the Go-owned platform directly. The old
Python service and pytest runtime suites are no longer part of the normal
verification path on this branch.

For the exact verification matrix, use
[docs/public/reference/local-testing.md](docs/public/reference/local-testing.md).
This file is the shorter overview.

## Quick Start

Fast local pass:

```bash
./tests/run_tests.sh fast
```

## Layer Breakdown

### Go unit and package tests

Parser extraction, query handlers, runtime wiring, storage contracts, and
domain materialization. No external services needed.

```bash
cd go
go test ./internal/parser ./internal/query ./internal/runtime ./internal/reducer ./internal/projector -count=1
```

### CLI and service wiring

The top-level CLI and runtime binaries should build and their focused tests
should pass.

```bash
cd go
go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1
```

### Deployment asset tests

Docker, Helm, and compose-backed runtime shape.

```bash
cd go
go test ./cmd/api ./cmd/mcp-server ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1
helm template eshu ./deploy/helm/eshu
kubectl kustomize deploy/manifests/minimal
```

### Docs smoke tests

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Local Service Stack

The default Docker Compose stack mirrors the production lifecycle with
NornicDB, Postgres, API, MCP, ingester, reducer, and bootstrap services:

1. Start NornicDB and Postgres.
2. Apply data-plane schema through `db-migrate`.
3. Prepare the workspace volume.
4. Run bootstrap indexing.
5. Start API, MCP, ingester, and reducer services.

```bash
docker compose up --build
```

If the default ports are in use:

```bash
NEO4J_HTTP_PORT=17474 \
NEO4J_BOLT_PORT=17687 \
ESHU_HTTP_PORT=18080 \
docker compose up --build
```

The fixture ecosystems used by the stack live under `tests/fixtures/ecosystems/`.

When you point Compose at host repositories, set `ESHU_FILESYSTEM_HOST_ROOT` to
an absolute real directory. Do not use symlinks, and do not use macOS `/tmp`
because Docker resolves it through `/private/tmp`.

Use `docker-compose.neo4j.yml` only for Neo4j compatibility checks.

## What We Verify

- parser extraction and matrix parity
- CLI behavior and flags
- MCP routing and tool exposure
- HTTP API and OpenAPI contract stability
- deployment artifacts for the public chart and minimal manifests
- compose-backed ingester and reducer flows
- Terraform provider-schema loading and relationship extraction

## Minimum Always-Run Gates

```bash
cd go
go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1
go test ./internal/parser ./internal/collector ./internal/collector/discovery ./internal/content/shape -count=1
go test ./internal/terraformschema ./internal/relationships ./internal/runtime ./internal/status ./internal/storage/postgres -count=1
git diff --check
```

## Current Gaps

- Cloud validation still depends on the deployment environment being available
- Compose-backed end-to-end proof remains slower than the focused package gates
- Parser parity should continue to be hardened against the full fixture matrix
