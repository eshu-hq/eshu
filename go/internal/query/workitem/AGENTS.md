# Work-Item Evidence — Agent Instructions

Scope: `go/internal/query/workitem/` (package `workitem`).

## Ownership

This leaf owns the source-only work-item evidence read route (#6642):
`evidence.go` (`EvidenceStore`, `EvidenceFilter`, `EvidenceRow`, decode
dispatch), `handler.go` (`Handler`, `Mount`, dispatch), `handler_tracing.go`
(the span seam), `scope.go` (the empty-grant page writer), `store.go`
(`PostgresEvidenceStore` and `NewPostgresEvidenceStoreWithReadStore`), `sql.go` (`listWorkItemEvidenceQuery`), `page.go`
(`EvidencePage`, pagination), `read_kinds.go` (`EvidenceFactKinds`),
`state.go` (evidence-state classification), and `capability.go`
(`EvidenceSupport`), plus this package's own tests, moved in verbatim from
root -- see README.md's Move evidence. The nine `work_item.*` typed decode
wrappers live in the shared `internal/query/decode/workitem` leaf (#6623),
which this package imports as `workitemdecode`; `evidence.go` keeps the
decode dispatch.

## Invariants

- MUST NOT import root package `query` -- root would import this package back
  for the compatibility aliases in `work_item_alias.go`, cycling. Reach
  root-only helpers through `querycontract` (profiles, envelopes, HTTP
  helpers, the repository-access-filter port), `auth` (scoped-token
  `AuthContext`), `tracing` (the shared handler-span seam), or `decode`
  (the classified decode-error type); if none of those has what you need, it
  does not belong here -- ask before adding a new shared home.
- `EvidenceCapability` MUST stay `"work_item.evidence.list"` -- byte-identical
  -- it is the route's registered capability id, read by
  `query/contract/work_item.go` (capability-matrix registration, off-limits
  to this lane per #6642 Part C) through the `workItemEvidenceCapability`
  forward in `work_item_alias.go`.
- `EvidenceFactKinds` MUST stay exactly `facts.WorkItemFactKinds()` -- root's
  `service_story_target_support.go` reads it through the
  `workItemEvidenceFactKinds` forward in `work_item_alias.go`, and this
  package's own SQL read bounds on it.
- `workitemHandlerTracer` is this package's own tracer var (the same seam
  `incidentHandlerTracer` in `go/internal/query/incident/handler.go` uses,
  also used by the sibling #6642 language move's `language/handler_tracing.go`'s
  `languageHandlerTracer`):
  package-local so a recording-provider span test stays private to this
  package. Do not promote it to an exported var or move the span helper back
  to root.
- The `*string` deref and the default schema version come from
  `decode.DerefString` and `decode.DefaultSchemaMajorVersion`, shared by every
  query-layer factschema decoder since #6642. Do not re-fork either here.
  The `*bool` deref, the decode input, the schema envelope, and the nine
  wrappers come from the shared `internal/query/decode/workitem` leaf
  (#6623); do not re-fork any of them here.
- A failed store read answers `querycontract.WriteGraphReadError` first, then
  `tracing.WriteServerFailure` with `workItemEvidenceListFailedMessage`.
  Never write `err.Error()` into a response body (#7674).

## Naming

`docs/internal/naming.md` is law: no `work_item_evidence_` file prefixes, no
`workitem/work_item_evidence.go`, exported identifiers lose the `WorkItem`
stutter except where the #6642 forwarder rule above names a specific root
caller. The root `work_item_alias.go` keeps every old exported spelling for
staying callers.

## Payload-usage manifest gate coverage (#4573)

The #4573 payload-usage manifest gate (`go test ./internal/reducer -run
TestPayloadUsageManifest`) discovers query-layer decode wrappers by globbing
`factschema_decode*.go` recursively under `go/internal/query`
(`go/internal/payloadusage/paths.go`, `QueryDecodeFiles`). The nine
`work_item.*` wrappers live in the shared
`internal/query/decode/workitem/factschema_decode.go` (#6623), not a
shorter name: the file name does not repeat the directory name, so rule 2
of `docs/internal/naming.md` is satisfied, and the gate keeps scanning the
wrappers after the move. This package calls them through the single
`workitemdecode` import spelling, the only qualifier the gate attributes;
do not rename that file or spelling without moving the glob and
`KnownDecodeQualifiers` together.
