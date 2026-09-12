# Work-Item Evidence

## Purpose

The source-only work-item evidence read route: `GET /api/v0/work-items/evidence`
(and the MCP `list_work_item_evidence` tool that shares the handler). Exposes
active Jira/work-item source facts directly for ticket-first prompts, without
verifying pull-request, commit, deployment, incident, runtime, image,
version, or service identity.

## Ownership boundary

Owns the `Handler` struct, its HTTP dispatch, the `EvidenceStore` Postgres
implementation, the typed `work_item.*` fact decode wrappers, and the
evidence-state classification, pagination, and span-attribute shaping this
route reports. Does not own the scoped-token grant type or context helpers
(`queryauth`), the content-model types, capability registry, or HTTP/error
envelope helpers (`querycontract`), or the classified decode-error type
(`decode`) -- those are separate leaves this package calls into.

## Layout

- `evidence.go` -- `EvidenceStore`, `EvidenceFilter`, `EvidenceRow`, the
  evidence-state constants, the route capability constant
  (`EvidenceCapability`) and the internal page-size bound
  (`evidenceMaxLimit`), the decode dispatch (`decodeWorkItemEvidenceRow`),
  and the URL-fingerprinting/sanitization helpers.
- `handler.go` -- `Handler`, `Mount`, `listWorkItemEvidence` and its request
  validation.
- `handler_tracing.go` -- the route's own span seam (`workitemHandlerTracer`,
  `startQueryHandlerSpan`), the same shape the other leaves keep in their
  `handler_tracing.go`.
- `scope.go` -- the empty-grant bounded zero-evidence page writer.
- `store.go` -- `PostgresEvidenceStore`, `NewPostgresEvidenceStore`, and the
  Postgres row-scan/pagination loop.
- `sql.go` -- `listWorkItemEvidenceQuery`, the scoped-token grant-bound SQL.
- `page.go` -- `EvidencePage` and `buildWorkItemEvidencePage`, the #4733
  truncation-from-fetched-not-decoded-count pagination contract.
- `read_kinds.go` -- `EvidenceFactKinds`, the fact-kind registry bound.
- `state.go` -- the evidence-state classification, summary, and span-attribute
  shaping.
- `factschema_decode.go` -- the nine `work_item.*` typed decode wrappers
  (`decodeWorkItemRecord`, `decodeWorkItemTransition`, ...), `workItemDecodeInput`,
  `workItemSchemaEnvelope`, and this package's own `derefString`/`derefBool`.
- `capability.go` -- `EvidenceSupport`, this family's capability contract.
- Test files -- this package's own tests, moved in verbatim from root (see
  Move evidence).

## Move evidence

The family moved here from the query root (`work_item_evidence*.go` and
`factschema_decode_workitem.go`'s work-item decoders, #6642, split off the
#6060 lane A restructure); the non-test files are destuttered
(`work_item_evidence.go` -> `evidence.go`, etc.) and every exported identifier
leading with `WorkItem` drops that word at its declaration
(`WorkItemHandler` -> `Handler`, `WorkItemEvidenceFilter` -> `EvidenceFilter`,
`WorkItemEvidencePage` -> `EvidencePage`, `WorkItemEvidenceRow` -> `EvidenceRow`,
`WorkItemEvidenceStore` -> `EvidenceStore`,
`PostgresWorkItemEvidenceStore` -> `PostgresEvidenceStore`,
`NewPostgresWorkItemEvidenceStore` -> `NewPostgresEvidenceStore`); every
method and unexported helper name is otherwise unchanged, except the two
deref helpers (`workItemDerefString`/`workItemDerefBool` -> local
`derefString`/`derefBool`, since this package's own decode wrappers use
`decode.Error`/`decode.New` directly instead of root's
`newQueryDecodeError` wrapper) and the internal page-size bound
(`workItemEvidenceMaxLimit` -> `evidenceMaxLimit`). Root keeps every pre-move exported spelling
through a stanza in the new `work_item_alias.go`: the `WorkItemHandler`,
`WorkItemEvidenceFilter`, `WorkItemEvidencePage`, `WorkItemEvidenceRow`,
`WorkItemEvidenceStore`, and `PostgresWorkItemEvidenceStore` type aliases, the
`NewPostgresWorkItemEvidenceStore` forwarder, and the
`workItemEvidenceCapability`/`workItemEvidenceFactKinds` unexported forwards
(`contract_work_item.go`'s capability-matrix registration and
`service_story_target_support.go` are the callers that need those two
unexported root spellings; see AGENTS.md).

`factschema_decode_workitem.go` itself stayed in root as
`factschema_decode_shared.go` (`git mv`): it kept the pieces
`factschema_decode_supplychain.go` still needs (`queryDecodeError`,
`newQueryDecodeError`, `queryDefaultSchemaMajorVersion`, and the renamed
`derefString`; the `derefBool` twin was dropped, since no root caller needed
it any more) and shed the nine `work_item.*` decode wrappers,
which moved into this package's `factschema_decode.go` using `decode.Error`/
`decode.New` directly and this package's own `defaultSchemaMajorVersion`
literal (duplicated rather than imported, since this package must not import
root).

`work_item_evidence_scope_test.go` split: the two
`TestAuthMiddlewareWithScopedTokens*WorkItem*` tests exercise root's own auth
middleware and the root-native `fakeScopedTokenResolver` fixture, so they
stayed in root's new `auth_scoped_routes_work_item_test.go`; the rest moved
here as `scope_test.go` using `queryauth.AuthContext`/
`queryauth.ContextWithAuthContext`/`queryauth.AuthModeScoped` and
`querycontract.ProfileProduction` directly.

No-Regression Evidence: baseline `origin/main` vs this branch -- `go test
./internal/query/...` and `go test ./internal/query/workitem/` pass, and their
combined test-name union carries every pre-move name from the `go test
./internal/query/ -list '.*'` baseline (2815 names, no duplicate, no drop);
the only addition is `TestWorkItemEvidenceCapabilityLockstep`
(`capability_lockstep_work_item_test.go`, root package `query`), the drift
guard the capability-lockstep review finding added, bringing the union to
2816; `go list -deps ./internal/query/workitem` names no `internal/query`
(root) dependency; the `internal/query` dirgate ledger row drops 490 -> 483
(eight non-test files left, one alias file arrived; the new lockstep test
file does not move this count, since dirgate excludes `*_test.go`).

No-Observability-Change: the span this route emits keeps its name
(`telemetry.SpanQueryWorkItemEvidence`) and `http.route`/`eshu.capability`
attributes, and the per-state span counters
(`telemetry.SpanAttrWorkItemEvidence*`) are unchanged.
`workitemHandlerTracer` is this package's own package-local tracer var
(mirroring `incidentHandlerTracer` in `go/internal/query/incident/handler.go`;
also used by the sibling #6642 language move's `language/handler_tracing.go`'s
`languageHandlerTracer`), seeded from `queryspan.HandlerTracer()`.

## Related docs

- `docs/internal/design/5584-route-serves-data-registry.md`
- `sdk/go/factschema/workitem/v1/README.md`
- `go/internal/query/evidence-notes.md`
- `go/internal/query/read-models.md`
