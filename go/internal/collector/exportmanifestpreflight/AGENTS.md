# AGENTS.md - collector/exportmanifestpreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/exportmanifestpreflight/README.md`
2. `go/internal/collector/exportmanifestpreflight/doc.go`
3. `go/internal/collector/exportmanifestpreflight/preflight.go`
4. `docs/internal/design/1741-1748-google-workspace-and-external-export-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, provider
  calls, archive unpacking, fact emission, storage calls, graph writes, API/MCP
  routes, goroutines, runtime knobs, or telemetry side effects belong here.
- Return metadata-only decisions. Do not persist source scope IDs, source
  cursors, issue keys, channel names, message IDs, file paths, private URLs,
  user names, tenant names, source item IDs, credentials, tokens, or export
  content.
- Preserve low-cardinality warning classes. Do not add raw source systems,
  paths, URLs, usernames, workspace names, tenant names, or provider IDs to
  warning fields.
- Treat a clean manifest preflight as necessary but not sufficient for
  ingestion. Full importers still need parser tests, fact readback proof, ACL
  handling, telemetry, and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a source-system enum only after the external-export design includes it.
- Add a new count only when it can be derived from manifest metadata without
  opening listed files.
- Add runtime telemetry only from a caller. This package should stay
  side-effect free.

## Failure modes

- Malformed JSON, invalid ACL enum values, or missing revision metadata returns
  `export_manifest_invalid`.
- Empty file lists return `allowlist_required` and `allowlist_empty`; missing
  source scopes return `allowlist_required`.
- Broad live-provider-style scopes return `allowlist_unsupported_scope`.
- Missing, partial, or unavailable ACL evidence returns explicit ACL warnings.
- Unsafe paths, nested archives, credential-looking paths, attachment members,
  and private channel metadata return design-owned warning classes.
- Duplicate source item IDs and token-bearing URLs are counted without returning
  the raw ID or URL.
- Caller cancellation or deadline returns `timeout` and no trusted safe result.

## Anti-patterns

- Opening files listed in the manifest.
- Treating manifest paths as filesystem authority.
- Recording raw paths, source scopes, provider IDs, ticket IDs, URLs, or names
  in result payloads.
- Adding live provider clients, fact emission, graph truth, API/MCP readback, or
  runtime flags without a separate security-reviewed implementation slice.
