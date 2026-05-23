# Developing Eshu

This document is for anyone writing code in this repo. It covers how the
current runtime is organized, where to find the deeper runbooks, and how to
validate changes without copying the public docs into another file.

For general contribution rules, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Canonical Docs

Use these as the primary source of truth while you work:

- [Local Testing Runbook](docs/public/reference/local-testing.md)
- [Docker Compose](docs/public/run-locally/docker-compose.md)
- [Service Runtimes](docs/public/deployment/service-runtimes.md)
- [Telemetry Overview](docs/public/reference/telemetry/index.md)

## Development Environment

Build and test from the Go module root:

```bash
go version
cd go && go test ./cmd/eshu -count=1
```

Use [Local Testing Runbook](docs/public/reference/local-testing.md) for the
smallest gate that proves the surface you touched. README and docs changes also
need the MkDocs strict build and `git diff --check`.

## Go Engineering Rules

This repository follows Google-aligned Go engineering defaults:

- start with tests for behavior changes
- write package and exported-symbol documentation that explains runtime intent,
  not just mechanics
- keep files under 500 lines; split modules before they become hard to review
- run `gofmt`, focused `go test`, and `golangci-lint` before calling work ready

If you change local verification or deployment behavior, update the matching
runbook in `docs/public/` in the same slice.

## Package Ownership

The high-level flow is:

```text
sync -> discover -> parse -> emit facts -> enqueue work -> reducer -> graph/content projection -> query surface
```

Keep changes inside the package that owns the behavior:

- `go/internal/collector/` owns repository collection, discovery, snapshotting,
  and parser inputs.
- `go/internal/parser/` owns parser registry, adapters, language behavior, and
  SCIP support.
- `go/internal/facts/` owns durable fact models and queue contracts.
- `go/internal/storage/postgres/` owns facts, queues, status, content, recovery,
  and decision storage.
- `go/internal/storage/cypher/` owns backend-neutral graph write contracts.
- `go/internal/reducer/` owns cross-domain materialization and shared
  projection.
- `go/internal/query/` owns HTTP handlers, OpenAPI, and bounded read surfaces.
- `go/internal/runtime/`, `go/internal/status/`, and `go/internal/telemetry/`
  own probes, status, retry policy, metrics, spans, and logs.

Use [Source Layout](docs/public/reference/source-layout.md) and
package-local `README.md` files for the directory-level map.

## Parser And IaC Work

Infrastructure parsing is split deliberately:

- `go/internal/parser/` handles raw file parsing and semantic extraction
- `go/internal/relationships/` handles repo-to-repo and infra relationship
  discovery
- `go/internal/terraformschema/` owns packaged Terraform provider schemas,
  identity-key inference, and category classification

Terraform provider schemas are runtime assets, not just generated fixtures. If
you change how Terraform extraction works, update the packaged schema path and
the operator docs together.

For language support, use
[Language Support](docs/public/contributing-language-support.md) and the
language pages under `docs/public/languages/` instead of duplicating the parser
matrix here.

## Adding A Parser Capability

1. Write or extend a fixture under `tests/fixtures/`.
2. Add a focused Go test under `go/internal/parser/`.
3. Implement the parser change in `go/internal/parser/`.
4. If the change affects relationship extraction or content shaping, add the
   corresponding test under `go/internal/relationships/` or
   `go/internal/content/shape/`.
5. Update the affected docs under `docs/public/`.

### Rules

- Start with tests.
- Keep parser/runtime ownership in Go.
- Do not add a compatibility bridge or resurrect deleted Python modules.
- Keep normal-path parser semantics inside the native engine rather than in the
  CLI or collector shells.

## Runtime Development

The service boundary is explicit:

- `go/cmd/api/`
- `go/cmd/mcp-server/`
- `go/cmd/bootstrap-index/`
- `go/cmd/ingester/`
- `go/cmd/reducer/`
- `go/cmd/eshu/`

Shared runtime concerns live under:

- `go/internal/runtime/`
- `go/internal/status/`
- `go/internal/telemetry/`
- `go/internal/storage/`

If a change affects probes, retries, admin/status, or recovery, update both the
runtime package tests and the operator docs.

## Integration Proof

Use [Local Testing Runbook](docs/public/reference/local-testing.md) to choose
focused package tests, Compose proofs, Helm checks, docs gates, and hot-path
performance evidence. When mounting host repositories into Compose, use an
absolute non-symlink path for `ESHU_FILESYSTEM_HOST_ROOT`.
