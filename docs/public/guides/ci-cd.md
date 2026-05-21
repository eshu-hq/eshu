# CI/CD Integration

Catch dead code before it reaches main. Eshu can run in CI pipelines to flag
graph-detectable issues at pull request time without requiring manual review
for the mechanical checks.

## GitHub Actions example

```yaml
name: Code Quality
on: [pull_request]
jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go/go.mod
      - name: Build Eshu
        run: |
          cd go
          go build -o ../eshu ./cmd/eshu
      - name: Index the repo
        run: ./eshu index .
      - name: Check dead code
        run: ./eshu analyze dead-code --repo payments --exclude @app.route --fail-on-found
```

### What each step does

**Index the repo** — `eshu index .` parses source code, builds the call graph, and stores it locally. For a typical service repo this takes 10-30 seconds.

**Check dead code** — `eshu analyze dead-code --repo payments --limit 200 --exclude @app.route --fail-on-found` finds derived dead-code candidates from the graph-backed candidate set after the current default entrypoint, Go public-API, test, and generated-code exclusions and any decorator exclusions are applied. `--repo` accepts a canonical ID, repository name, repo slug, or indexed path, so CI and humans do not need to discover the canonical repository ID first. Use `--limit` to control the bounded result window; the command output reports `truncated=true` when more candidates existed than were returned. The command exits non-zero when candidates remain, failing the PR check.

Threshold-based complexity gating is available through the Go CLI today via
`eshu analyze complexity`. If you want CI to enforce a threshold, treat that as
an optional policy layer on top of the shipped command rather than a missing
runtime-parity feature.

## Excluding paths with .eshuignore

Some directories inflate the graph without adding signal. Create a `.eshuignore` file at your repo root:

```
tests/fixtures/
docs/
scripts/
*.generated.py
```

Syntax follows `.gitignore` patterns. See the [.eshuignore reference](../reference/eshuignore.md) for details.

## Large-scale indexing

For repositories with 100,000+ lines of code:

1. **Use the default NornicDB or explicit Neo4j stack** — do not use retired
   local-only graph backends for large graphs
2. **Increase graph/backend memory when needed** — tune the backend you selected
3. **Exclude test fixtures** — add `tests/` to `.eshuignore` if test code inflates the graph without adding signal
4. **Reuse stable artifacts** — cache the built Eshu binary and any database or bundle artifacts your pipeline already produces, instead of rebuilding them in every stage

## See also

- [CLI Analysis Reference](../reference/cli-analysis.md) — all `eshu analyze` subcommands
- [Configuration](../reference/configuration.md) — environment variables and settings
- [.eshuignore](../reference/eshuignore.md) — ignore file syntax
- [Bundles](bundles.md) — import and search prebuilt graph bundles
