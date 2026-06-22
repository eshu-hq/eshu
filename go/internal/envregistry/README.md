# envregistry

Code-owned source of truth for Eshu's `ESHU_*` environment variables.

## Purpose

Eshu reads ~hundreds of `ESHU_*` variables from scattered `os.Getenv` call
sites. Before this package there was no central registry, no startup validation,
and inconsistent naming (`ESHU_POSTGRES_DSN` vs `ESHU_CONTENT_STORE_DSN`), so
misconfiguration failed late with unhelpful errors. This package declares the
supported core-platform variables once and powers:

- `eshu config validate` — checks the live environment for invalid values,
  deprecated variables, and likely typos.
- `docs/public/reference/env-registry.md` — generated reference doc.

## What it covers

The registry covers the **core platform** subsystems (`postgres`, `graph`,
`runtime`, `api`, `mcp`, `reducer`, `projector`, `coordinator`, `semantic`,
`component`) declared in `entries.go`, and the **hosted-collector** production
configuration declared in `entries_collectors.go` (one `collector-*` subsystem
per collector). Container-registry credential variables (`ESHU_*_OCI_*`,
`ESHU_*_PACKAGE_*`) are integration-test gating read only from `_test.go` and are
out of scope.

`TestRegistryCoversCoreEnvCallSites` is the CI gate: it scans the canonical core
and collector config files (`coreScanFiles`) and fails if any `ESHU_*` they read
is missing from the registry. This keeps the registry from drifting away from the
code it documents, scoped honestly to what it claims to cover.

No-Regression Evidence: this package is pure declarations plus validation
helpers, read only by `eshu config validate` and the doc-generator test — never
on any service request, graph, reducer, or queue hot path. The collector entries
add no runtime behavior; trigger words in their descriptions (e.g. "heartbeat")
describe collector settings and are not executed here. Verified by
`go test ./internal/envregistry ./cmd/eshu -count=1`.

No-Observability-Change: extends a CLI validation command and a generated
reference doc; emits no metrics, spans, or logs from any running service.

## Exported surface

- `Entry` — one variable declaration (name, type, default, subsystem,
  description, allowed enum values, aliases, deprecation).
- `Registry` / `New` / `Default` — immutable lookup over a set of entries,
  indexed by canonical name and alias.
- `Registry.Validate(env, strict)` — returns `Finding`s (invalid value,
  deprecated, unknown).
- `Registry.RenderMarkdown` — deterministic reference-doc rendering.
- `VarType` constants — `VarString`, `VarInt`, `VarBool`, `VarDuration`,
  `VarEnum`, `VarDSN`.

## Aliases and deprecations

- DSN precedence: `ESHU_FACT_STORE_DSN` → `ESHU_CONTENT_STORE_DSN` →
  `ESHU_POSTGRES_DSN` (declared as aliases of the canonical `ESHU_POSTGRES_DSN`).
- Graph: `ESHU_NEO4J_*` fall back to legacy non-prefixed `NEO4J_*` (declared as
  aliases).
- Deprecated: `ESHU_REDUCER_CLAIM_DOMAIN` → `ESHU_REDUCER_CLAIM_DOMAINS`;
  legacy alias `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS` →
  `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED`.

## Maintaining the registry

1. Add or change an `Entry` in `entries.go`. If the variable lives in a new
   split config file, add that file to `coreScanFiles` so the coverage test
   protects it.
2. Regenerate the reference doc:
   `ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry -run TestEnvRegistryReferenceDocUpToDate`.
3. Run `go test ./internal/envregistry -count=1`. If you added a variable read
   in a `coreScanFiles` file, the coverage test enforces it is declared.

## Runtime impact

No-Regression Evidence: #3464 only adds `ESHU_SCOPED_TOKENS_FILE` to the
registry metadata, one generated reference-doc row, and a lookup regression
test. Baseline and after measurement are the same runtime shape: no service
request, graph-write, reducer, queue, goroutine, channel, worker, lease, or
Cypher path changes, and no backend/version-specific input shape. Terminal graph
row counts and queue counts are unchanged because the registry is read by
`eshu config validate` and the doc-generator test, not by API/MCP auth
resolution. Verified by `go test ./internal/envregistry ./cmd/eshu -count=1`,
`golangci-lint run ./...`, and `scripts/verify-performance-evidence.sh`.

No-Observability-Change: #3464 adds no metrics, spans, log fields, status
payload fields, or telemetry labels. Operator-visible evidence remains the
validation output and generated environment-variable docs; API/MCP auth
telemetry is owned by the runtime auth code, which is unchanged here.
