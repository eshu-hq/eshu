# AGENTS.md - evidencebundle

## Ownership

This package owns only the pure `evidence_bundle.v1` schema, its two
deterministic builders (the demo fixture builder and the live builder that
shapes an already-fetched status snapshot), the JSON renderer, and validation
logic. It must not open stores, call providers, query graph backends, run
MCP/API calls, read private source files, or export whole database state.

`BuildLiveBundle` takes a `LiveSnapshot` the caller already resolved. Fetching
the status endpoints that fill it belongs to `cmd/eshu`, not here.

## Rules

- Keep output deterministic; sort externally visible slices before rendering.
- Keep bundles share-safe: handles, route/tool/command names, schema versions,
  freshness states, and missing-evidence reasons are allowed.
- Reject private endpoints, credentials, prompts, provider responses, raw source
  payloads, and local absolute paths. Validation also asserts the shared
  hosted-governance redaction registry, which is the canonical taxonomy; this
  package's own patterns are a supplement to it, not a replacement.
- Preserve missing evidence as explicit data; do not hide gaps by deleting rows.
- Add tests before changing schema, validation canaries, or render shape.
