# Contributing to Eshu

Contributions are welcome. Small, well-scoped pull requests are preferred over
broad refactors.

## Guidelines

- Keep pull requests focused on a single problem or feature.
- Match the existing Go-first runtime architecture and package boundaries.
- Add or update tests when behavior changes.
- Keep handwritten source files under the repo line limit.
- Call out follow-up work separately instead of bundling unrelated cleanup.
- Do not reintroduce Python runtime ownership on this migration branch.

## Development Setup

1. Fork the repository.
2. Install the development environment:

```bash
go version
uv sync
```

3. Create a branch for your work:

```bash
git checkout -b feature/my-change
```

## Running Checks

Before opening a pull request:

```bash
cd go
go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1
go test ./internal/parser ./internal/collector ./internal/query ./internal/runtime ./internal/reducer ./internal/projector -count=1
go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1
golangci-lint run ./...
git diff --check
```

See [TESTING.md](TESTING.md) for the full test strategy and layer breakdown.

## Submitting Changes

1. Make the smallest change that solves the problem.
2. Run the checks above locally.
3. Commit with a clear message.
4. Open a pull request against `main`.

## Maintainer

Eshu is maintained by Allen Sanabria. GitHub:
[@linuxdynasty](https://github.com/linuxdynasty)
