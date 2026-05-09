# AGENTS.md - internal/collector/confluence guidance for LLM assistants

## Read First

1. `go/internal/collector/confluence/README.md` - package purpose, flow, and
   invariants
2. `go/internal/collector/confluence/source.go` - `Source.Next`, generation
   construction, partial-sync handling, and fact envelope creation
3. `go/internal/collector/confluence/client.go` - read-only HTTP client and
   permission-gap behavior
4. `go/internal/collector/confluence/config.go` - env config validation
5. `go/internal/facts/documentation.go` - source-neutral documentation fact
   schema

## Invariants This Package Enforces

- **Read-only source evidence** - Confluence access must remain `GET` only.
  Eshu collectors gather truth; write behavior belongs in separate services.
- **Bounded syncs only** - collection must be scoped to a space ID or root page
  ID. Do not introduce unbounded site-wide crawling.
- **Source-neutral output** - facts must use the documentation schema, not
  Confluence-specific fact kinds.
- **Partial-sync visibility** - permission gaps must increment
  `failure_count` and set `sync_status=partial`.
- **Stable identity** - page identity must be based on Confluence page ID, not
  title, so duplicate titles remain distinct.

## Common Changes And How To Scope Them

- **Add Confluence metadata** - extend `Page` or `Space`, add a fixture or fake
  test, and map it into documentation payload metadata only when the source
  returns it.
- **Change link extraction** - add HTML storage-body tests first. Preserve
  deterministic `LinkID` ordering.
- **Change stale-page handling** - update tests for deleted pages and duplicate
  revisions before changing `latestCurrentPages`.
- **Change client behavior** - keep HTTP tests proving read-only methods and
  permission-gap mapping.

## Anti-Patterns

- Calling Confluence mutation APIs.
- Creating Confluence-specific fact kinds for data already represented by
  documentation source, document, section, or link facts.
- Failing an entire page-tree sync for a single inaccessible child page.
- Treating empty spaces as errors.
