# AGENTS.md - evidencebundle

## Ownership

This package owns only the pure `evidence_bundle.v1` schema, deterministic demo
builder, JSON renderer, and validation logic. It must not open stores, call
providers, query graph backends, run MCP/API calls, read private source files, or
export whole database state.

## Rules

- Keep output deterministic; sort externally visible slices before rendering.
- Keep bundles share-safe: handles, route/tool/command names, schema versions,
  freshness states, and missing-evidence reasons are allowed.
- Reject private endpoints, credentials, prompts, provider responses, raw source
  payloads, and local absolute paths.
- Preserve missing evidence as explicit data; do not hide gaps by deleting rows.
- Add tests before changing schema, validation canaries, or render shape.
