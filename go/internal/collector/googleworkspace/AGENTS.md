# AGENTS.md - internal/collector/googleworkspace guidance for LLM assistants

## Read first

1. `go/internal/collector/googleworkspace/README.md`
2. `go/internal/collector/googleworkspace/doc.go`
3. `go/internal/collector/googleworkspace/collect.go`
4. `docs/internal/design/1741-1748-google-workspace-and-external-export-ingestion.md`
5. `go/internal/facts/documentation.go`

## Invariants

- Keep this package default-off. Do not add live HTTP clients, credentials,
  runtime flags, Helm values, Compose profiles, or public operator docs here.
- Require explicit file, folder, or shared-drive allowlists. Blank config never
  means all Drive.
- Emit source-neutral documentation facts only. Do not create graph edges,
  service truth, deployment truth, incident truth, ownership truth, entity
  mentions, or claim candidates from Workspace text.
- Keep raw file IDs, principals, tenant domains, private URLs, token-bearing
  links, and export byte streams out of emitted metadata unless redacted or
  fingerprinted.
- Treat per-file failures as document evidence with bounded failure classes.

## Common changes and how to scope them

- Add a failure class by adding tests first, extending `FailureClass`, and
  mapping it in `classifyFailure`.
- Add a Workspace file family by extending `FileKind`, `exportMIME`, and
  section tests. Do not add renderer or conversion dependencies here.
- Add runtime collection only in a separate reviewed issue with telemetry,
  status, credentials, and deployment docs.

## Failure modes

- Invalid or blank allowlists return no facts and no provider calls.
- Permission, deletion, stale revision, rate/quota, download-denied, and size
  failures emit metadata-only document facts.
- ACL partials stay in `acl_summary` and source metadata as document evidence.

## Anti-patterns

- Using Google provider SDKs or network calls in this package.
- Logging raw provider metadata or putting source IDs in metric labels.
- Adding query, MCP, reducer, graph, or storage imports.

## What not to change without review

Live collection, credential loading, Drive pagination, shared-drive query
semantics, CSV sheet slicing, public docs, runtime telemetry, and chart/Compose
enablement require a separate security-reviewed design update.
