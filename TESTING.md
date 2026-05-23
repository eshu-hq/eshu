# Testing Eshu

Eshu validates the Go-owned platform directly. Python appears only in fixtures
or offline tooling, not as a normal runtime service.

For the exact verification matrix, use
[docs/public/reference/local-testing.md](docs/public/reference/local-testing.md).
This file is the shorter overview.

## Fast Local Check

Fast local pass:

```bash
./tests/run_tests.sh fast
```

## Layer Breakdown

| Layer | What it proves | Where to look |
| --- | --- | --- |
| Go package tests | Parser extraction, query handlers, runtime wiring, storage contracts, and domain materialization. | `go/internal/*`, `go/cmd/*` |
| CLI and service wiring | The public CLI and service binaries build and keep focused contracts. | `go/cmd/README.md` |
| Deployment assets | Compose, Helm, and minimal manifest shape. | `docs/public/reference/local-testing.md` |
| Documentation | Public nav, links, and documentation truth claims. | `docs/mkdocs.yml`, `go run ./cmd/eshu docs verify` |

## Common Gates

Use [Local Testing Reference](docs/public/reference/local-testing.md) as the
source of truth. These are common entry points, not a replacement for that
matrix.

Go package or command slice:

```bash
cd go
go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./internal/query -count=1
```

Deployment shape:

```bash
(cd go && go test ./cmd/api ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1)
helm template eshu ./deploy/helm/eshu
kubectl kustomize deploy/manifests/minimal
```

Docs:

```bash
(cd go && go run ./cmd/eshu docs verify .. --limit 1400 --fail-on contradicted,missing_evidence)
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

## Local Service Stack

Use [Docker Compose](docs/public/run-locally/docker-compose.md) for the current
service list, ports, profiles, overlays, and host-repository mount rules. The
default stack uses NornicDB and Postgres. Use `docker-compose.neo4j.yml` only
for Neo4j compatibility checks.

## What We Verify

- parser extraction and matrix parity
- CLI behavior and flags
- MCP routing and tool exposure
- HTTP API and OpenAPI contract stability
- deployment artifacts for the public chart and minimal manifests
- compose-backed ingester and reducer flows
- Terraform provider-schema loading and relationship extraction

## Current Gaps

- Cloud validation still depends on the deployment environment being available
- Compose-backed end-to-end proof remains slower than the focused package gates
- Parser parity should continue to be hardened against the full fixture matrix
