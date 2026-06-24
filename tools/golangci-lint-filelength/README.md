# golangci-lint-filelength

A small [Go plugin](https://golangci-lint.run/docs/plugins/go-plugins) for
[golangci-lint v2.11.4](https://github.com/golangci/golangci-lint) that
enforces Eshu's repository-wide **500-line file cap** on non-test,
non-generated Go source files.

The cap is a load-bearing rule for Eshu — it is what keeps the
collector → reducer → graph hot path reviewable and the
package-`doc.go`/`README.md`/`AGENTS.md` triple maintainable. The
audit recorded in `docs/internal/audit/2026-06-09-repo-technical-audit.md`
calls out the 29 files that already break the rule; this plugin exists
so new violations fail CI immediately.

## What it checks

For every Go file in the package syntax tree:

| Condition                            | Skipped? |
| ------------------------------------ | -------- |
| Filename ends in `_test.go`          | yes      |
| Path contains `/testdata/`           | yes      |
| Path contains `/vendor/`             | yes      |
| Path contains `/generated/`          | yes      |
| All other `.go` files                | checked  |

A file is reported (single diagnostic at the file's `Pos`) when its
physical line count exceeds **500**.

## Why a Go plugin

golangci-lint v2 removed the `linters.custom` config section that
existed in v1. The supported way to add a private linter is now one
of:

1. A Go plugin (`.so` file) loaded via
   `linters.settings.custom.<name>.path`, or
2. A separate Go module registered via
   `golangci-lint custom` (which clones the golangci-lint source and
   builds a custom binary — a few minutes of CI time per build).

This plugin uses option 1 because:

- The plugin adds about 1.5 s of CI time (a single `go build
  -buildmode=plugin`) instead of cloning + rebuilding golangci-lint.
- The plugin source is a normal Go package that is testable with
  `go test`, so we can apply TDD to the cap logic without spinning
  up golangci-lint.
- The plugin is loaded as a standard `*analysis.Analyzer` by
  golangci-lint, so the diagnostics flow through the same
  processors, exclusions, and `--max-issues-per-linter` rules as
  every other linter.

## Version pinning

The plugin must be built against the **same** `golang.org/x/tools`
revision that golangci-lint v2.11.4 vendors. A mismatch makes
`plugin.Open` fail with:

```text
plugin was built with a different version of package
golang.org/x/tools/go/analysis
```

`go.mod` pins `golang.org/x/tools v0.43.0` (the revision in
`go/pkg/mod/github.com/golangci/golangci-lint/v2@v2.11.4/go.mod`).
When bumping golangci-lint, bump this pin in the same PR.

## Build and test

```bash
make build   # produces filelength.so
make test    # runs the unit tests for skip() / countLines() / New()
make clean   # removes the .so
```

## How CI uses it

`go/.golangci.yml` lists the plugin under `linters.settings.custom`:

```yaml
linters:
  settings:
    custom:
      filelength:
        type: goplugin
        path: ../tools/golangci-lint-filelength/filelength.so
        description: "Eshu 500-line file cap"
        original-url: github.com/eshu-hq/eshu/tools/golangci-lint-filelength
```

`.github/workflows/test.yml` builds the plugin before invoking
`golangci-lint run ./...`:

```yaml
- name: Build golangci-lint filelength plugin
  working-directory: tools/golangci-lint-filelength
  run: make build

- name: Install golangci-lint
  run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4

- name: Lint Go
  working-directory: go
  run: golangci-lint run ./...
```

## Exempting a file

The cap is a hard rule for new code. For files that already exceed
500 lines (the audit records the inventory) the standard
`//nolint:filelength` directive applies:

```go
//nolint:filelength // 685-line contract doc; see docs/internal/agent-guide.md § Service Boundaries
package telemetry
```

A blanket `//nolint:filelength` is the right call only when the
file is a data registry (`instruments.go`), a generated adapter, or
a deliberately-bundled contract doc. Every other exemption must
cite a tracking issue that will split the file.
