# confluence Agent Guidance

## Read First

1. `README.md` and `doc.go` for package scope.
2. `config.go` for bounded source modes.
3. `client.go` for read-only HTTP behavior, pagination, and permission gaps.
4. `source_collect.go` for page collection, enrichment, and partial-sync
   behavior.
5. `source.go`, `extract.go`, and `source_observed.go` for fact emission,
   link extraction, claim candidates, and observation telemetry.
6. `go/internal/facts/documentation.go` for source-neutral documentation facts.

## Local Rules

- Keep Confluence access read-only. HTTP clients must only issue `GET`
  requests.
- Require one bounded source: one space, an explicit space-ID allowlist, or one
  root page tree. Do not add site-wide crawling.
- Emit source-neutral documentation facts; do not create Confluence-specific
  fact kinds for data already covered by source, document, section, or link
  facts.
- Treat permission gaps as partial-sync evidence with failure counts and
  `sync_status=partial`; do not fail the whole tree for one inaccessible child.
- Keep page identity based on Confluence page ID, not title.
- Keep page IDs, titles, URLs, paths, body text, excerpts, credentials, and
  claim text out of metric labels.
- Keep pagination bounded to the configured Confluence base URL and compatible
  `/wiki` context path handling.

## Change Rules

- Add metadata only when the source returns it; cover it with fake or fixture
  tests before mapping it into documentation payload metadata.
- Change link extraction with storage-body HTML tests and deterministic
  `LinkID` ordering.
- Change stale-page handling with deleted-page and duplicate-revision tests.
- Change client behavior with tests proving read-only methods and
  permission-gap mapping.
- Add telemetry through bounded labels and update metric tests plus telemetry
  docs when names or labels change.
