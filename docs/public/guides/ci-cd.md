# CI/CD Integration

Catch dead code before it reaches main. Eshu can run in CI pipelines to flag
graph-detectable issues at pull request time without requiring manual review
for the mechanical checks.

## GitHub CodeQL setup

Eshu's repository-owned CodeQL model is GitHub default setup only. Do not check
in an advanced CodeQL workflow or any workflow step using
`github/codeql-action/init`, `github/codeql-action/autobuild`,
`github/codeql-action/analyze`, `codeql database analyze`, or `codeql github
upload-results` while default setup remains active in repository or
organization settings. GitHub documents that [CodeQL SARIF uploads are rejected
while default setup is
enabled](https://docs.github.com/en/code-security/code-scanning/troubleshooting-sarif-uploads/default-setup-enabled),
so a checked-in CodeQL result upload would be stale PR signal instead of a
trustworthy gate.

The `github/codeql-action/upload-sarif` action is not forbidden by itself
because GitHub supports SARIF uploads from non-CodeQL tools. A workflow that
uses it for Eshu or another non-CodeQL scanner must keep CodeQL-generated SARIF
out of the upload path while this default setup model is active.

Go CodeQL is not currently claimed as an Eshu repository-owned required PR
check. The required Go correctness gates remain the normal build, lint, race
test, package-doc, hot-path evidence, Helm, docs, and whitespace checks in
`.github/workflows/test.yml`. If GitHub default setup scans Go, treat that as a
GitHub-managed security signal, not a checked-in workflow contract controlled
by this repository.

Moving to advanced CodeQL requires a single PR that first changes the repository
security setting away from default setup, then updates this CI contract,
`scripts/verify-codeql-setup.sh`, and the local testing reference. That PR must
prove the intended Go scope on this repository shape without treating fixture
modules or generated collector code as first-class application packages. GitHub
documents that [Go CodeQL uses compiled-language build and extraction
behavior](https://docs.github.com/en/code-security/reference/code-scanning/codeql/codeql-build-options-and-steps-for-compiled-languages#building-go),
so the advanced workflow decision must include explicit build-scope evidence.

The CI guard is:

```bash
scripts/test-verify-codeql-setup.sh
scripts/verify-codeql-setup.sh
```

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
