# Work-Item Evidence — Agent Instructions

Scope: `go/internal/query/workitem/` (package `workitem`).

## Ownership

This leaf owns the source-only work-item evidence read route (#6642):
`evidence.go` (`EvidenceStore`, `EvidenceFilter`, `EvidenceRow`, decode
dispatch), `handler.go` (`Handler`, `Mount`, dispatch), `handler_tracing.go`
(the span seam), `scope.go` (the empty-grant page writer), `store.go`
(`PostgresEvidenceStore`), `sql.go` (`listWorkItemEvidenceQuery`), `page.go`
(`EvidencePage`, pagination), `read_kinds.go` (`EvidenceFactKinds`),
`state.go` (evidence-state classification), `factschema_decode.go` (the nine
`work_item.*` typed decode wrappers), and `capability.go`
(`EvidenceSupport`), plus this package's own tests, moved in verbatim from
root -- see README.md's Move evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package back
  for the compatibility aliases in `work_item_alias.go`, cycling. Reach
  root-only helpers through `querycontract` (profiles, envelopes, HTTP
  helpers, the repository-access-filter port), `queryauth` (scoped-token
  `AuthContext`), `queryspan` (the shared handler-span seam), or `decode`
  (the classified decode-error type); if none of those has what you need, it
  does not belong here -- ask before adding a new shared home.
- `EvidenceCapability` MUST stay `"work_item.evidence.list"` -- byte-identical
  -- it is the route's registered capability id, read by root's
  `contract_work_item.go` (`init()` capability-matrix registration, off-limits
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
- `derefString`/`derefBool` (`factschema_decode.go`) are this package's OWN copy of the
  nil-safe pointer-deref helpers, deliberately not imported from root's
  `derefString` (`factschema_decode_shared.go`, whose only caller is
  `factschema_decode_supplychain.go`; root's own `derefBool` twin was dropped
  in #6642 since no root caller needed it any more): this package must not
  import root, and a two-line helper is not worth hoisting into
  `querycontract` for two callers. Do not re-fork a third copy elsewhere; if
  a third caller needs this, ask before adding a shared home.
- `defaultSchemaMajorVersion` (`factschema_decode.go`, `"1.0.0"`) is a deliberate
  duplicate of root's `queryDefaultSchemaMajorVersion`
  (`factschema_decode_shared.go`), for the same reason as the deref helpers.
  Keep both in sync if the schema-major convention ever changes; they cover
  disjoint fact families today (work-item here, supply-chain in root), so
  drift is unlikely but not structurally prevented.

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
`work_item.*` wrappers therefore live in `factschema_decode.go`, not a
shorter name: the file name does not repeat the directory name, so rule 2
of `docs/internal/naming.md` is satisfied, and the gate keeps scanning this
family after the move. Do not rename that file without moving the glob.
