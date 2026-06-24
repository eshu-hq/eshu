# golangci-lint Configuration

This document records the Eshu Go data plane's committed `golangci-lint`
configuration (issue eshu-hq/eshu#3761, parent Epic O eshu-hq/eshu#3733).
It is the tracked evidence file for the gate
`scripts/verify-performance-evidence.sh`; the marker headings below are
parsed by the gate and must remain present.

## Version pin

The config is pinned to golangci-lint v2.11.4 — the exact version
`.github/workflows/test.yml` installs via
`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4`.
The 500-line-cap Go plugin in `tools/golangci-lint-filelength/` is pinned
to `golang.org/x/tools v0.43.0` (the revision v2.11.4 vendors) for the
same reason: a Go plugin loaded via `plugin.Open` must be built against
the same `golang.org/x/tools` revision the host binary uses.

## Linter set

The config enables: `govet`, `staticcheck` (umbrella that pulls in
`ineffassign`, `unused`, `gosimple`), `gocritic`, `errcheck`,
`exhaustive`, `wrapcheck`. `gofumpt` is enabled under `formatters`
in v2's split model. Each linter's per-package settings and the
boundary-package exclusions (cmd/*, AWS SDK adapters, scanner
workers, OCI/package-registry sources, git selection filesystem,
webhook normalizers, recovery replay, scoped token identity, OIDC
login, ask wiring, search backends, exports registry, graph
adapters, MCP server, projector service, etc.) are documented
inline in `go/.golangci.yml`.

The 500-line file cap is delivered as a gocritic companion plugin
loaded via `linters.settings.custom.filelength`. The plugin is a
normal Go package, testable with `go test ./...`, and ships with
unit tests for its `skip` predicate and `countLines` helper.

## Evidence

### gofumpt reformatting (mechanical whitespace, ~106 files)

No-Regression Evidence: `gofumpt -w` was run across the tree to bring
the source into compliance with the new gofumpt gate. The change is
purely mechanical whitespace (multi-line function-argument alignment,
trailing newlines, blank-line normalization) and is byte-for-byte
reversible. The existing race-detected test suite (`go test ./...
-count=1 -race -timeout 300s -p 2`) passes both before and after the
reformat on every package that was touched. The 106 files touched are
all `go/...` Go source files; no generated files, vendor files, or
testdata fixtures were modified.

No-Observability-Change: `gofumpt -w` rewrites whitespace only. It
adds no metric instrument, metric label, span, log line, status
field, env var, queue, worker, lease, batch, runtime knob, or graph
query. Operators still diagnose the affected surfaces through
existing collector parse-stage logs, `eshu_dp_file_parse_duration_seconds`,
reducer run spans, reducer execution counters, durable
`reducer_*` payloads, query handler spans, and
`eshu_dp_postgres_query_duration_seconds` exactly as before.

### filelength nolint annotations (21 non-test files)

No-Regression Evidence: 21 non-test Go source files exceed the
500-line cap. Each carries a per-file `//nolint:filelength` directive
citing the reason (data registry, god file on hot path, AWS SDK
adapter, single-purpose file, etc.). The 21 files match the audit's
inventory at `docs/internal/audit/2026-06-09-repo-technical-audit.md`
(§ Architecture & design, "[fact, High] The repo violates its own
500-line rule on its most critical files"). The split work is
tracked separately under audit § T8; the lint annotations
preserve the cap as a forward-looking rule without blocking the
config adoption.

No-Observability-Change: the per-line `//nolint:filelength`
annotations are line markers that the existing linter pass already
skips. They add no runtime behavior, no metric, no log, no queue,
no worker, no batch, and no graph-write change. The audit confirms
`instruments.go` is a data registry and the other 20 files are
either god files tracked for split or single-purpose adapters.

### gocritic disabled-checks (31 checks)

No-Regression Evidence: 31 gocritic sub-checks are disabled in
`go/.golangci.yml` (`gocritic.disabled-checks`). Each entry cites
the style reason the codebase deliberately keeps the non-flagged
form (e.g. `paramTypeCombine`: "the audit notes the codebase
prefers the explicit form for readability and review"; `httpNoBody`:
"the codebase uses `nil` for clarity"). The disabled checks are
all stylistic; the 13 remaining `//nolint:gocritic` annotations
are per-line markers for contextually-correct code (evalOrder in
Bubble Tea animation, ptrToRefParam on a shared-error pattern,
offBy1 in tests guarded by preceding `t.Fatalf`, etc.).

No-Observability-Change: gocritic sub-checks are lint-time
advisories. Disabling a sub-check only affects which advisories the
linter emits; it does not change runtime behavior, metric surface,
log shape, queue behavior, lease contract, Cypher shape, or graph
schema.

### exhaustive + wrapcheck path-level exclusions

No-Regression Evidence: 30 exhaustive exclusions and 26 wrapcheck
exclusions are placed at the path level in
`linters.exclusions.rules`. The exhaustive exclusions target
switch statements that intentionally enumerate a subset of an
enum (reducer domain catalog, evidence-kind dispatch tables,
collector boundary switches, query freshness/replatforming state
handlers, etc.). The wrapcheck exclusions target boundary packages
where the wrap context is added at the handler level (AWS SDK
adapters, scanner workers, OCI/package-registry sources, git
selection filesystem, webhook normalizers, recovery replay,
scoped token identity, OIDC login, ask wiring, search backends,
exports registry, graph adapters, MCP server, projector service,
etc.). The audit confirms the codebase has consistent `%w` wrapping
discipline at every internal hop.

No-Observability-Change: exclusion rules are linter configuration;
they do not change runtime behavior, metric/span/log surface, queue
behavior, lease contract, Cypher shape, or graph schema.

## Verification

- `golangci-lint run ./...` exits 0 with the new config.
- `go test ./internal/queue ./internal/runtime ./internal/telemetry
  ./internal/recovery ./internal/reducer -count=1` green.
- `go test ./tools/golangci-lint-filelength/...` green (plugin unit
  tests for `skip` / `countLines` / `New`).
- `go build ./...` clean.
- `git diff --check` clean.
- The 500-line cap is enforced: a synthetic 501-line file under
  `internal/collector/` fails the run; `//nolint:filelength` on the
  package line suppresses it; a test file or a vendored file is
  skipped by the plugin's own `skip` predicate.

## Cross-references

- `go/.golangci.yml` — the committed config
- `tools/golangci-lint-filelength/` — the custom 500-line-cap plugin
  (Makefile, README, source, tests)
- `.github/workflows/test.yml` — the CI step that builds the plugin
  and runs `golangci-lint run ./...`
- `docs/internal/audit/2026-06-09-repo-technical-audit.md` — the
  audit that identified the 500-line cap violation (§ Architecture
  & design) and the god-file split work (§ T8)
- `docs/internal/agent-guide.md` — the Pre-Ready Checklist that
  requires evidence markers on hot-path changes
