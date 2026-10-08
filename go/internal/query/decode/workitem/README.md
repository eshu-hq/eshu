# Query work-item decode substrate

## Purpose

One home for the work-item decode wrappers every query read path shares.
Before #6623 the incident store carried verbatim forks of the work-item
family's decoders because it could not import them; a behavior change in one
copy silently invalidated the other. Both paths now execute this code.

## Ownership boundary

This leaf owns the nine `work_item.*` typed decode wrappers
(`factschema_decode.go`), the `DecodeInput` one-argument shape, the
`SchemaEnvelope` version normalizer, `DerefBool`, and the
`LogEvidenceDecodeDrop` helper (`drop.go`). It does not own any read model:
the work-item evidence dispatch stays in `internal/query/workitem`, and the
incident reads stay in `internal/query/incident/store`. It imports only the
stdlib, `log/slog`, `internal/query/decode`, and the factschema SDKs — never
the query root, `incident/`, the workitem family, or `querycontract`.

Naming debt, kept deliberately: the incident store calls `SchemaEnvelope`
for incident kinds too (it did so through its fork before the move). The
helper is kind-agnostic version normalization; renaming it generic would
churn both consumers for no behavioral gain.

## Surface

| Before (two copies) | After (this leaf) |
|---|---|
| `decodeWorkItemRecord` + 8 siblings | `DecodeRecord`, `DecodeTransition`, `DecodeExternalLink`, `DecodeProjectMetadata`, `DecodeIssueTypeMetadata`, `DecodeStatusMetadata`, `DecodeWorkflowMetadata`, `DecodeFieldMetadata`, `DecodeMetadataWarning` |
| `workItemDecodeInput` | `DecodeInput` |
| `workItemSchemaEnvelope` | `SchemaEnvelope` |
| `derefBool` / `workItemDerefBool` | `DerefBool` |
| `logWorkItemEvidenceDecodeDrop` | `LogEvidenceDecodeDrop` |

Importers use one spelling: `workitemdecode "…/query/decode/workitem"`.
The payload-usage manifest gate keys qualified decode calls on that
qualifier; see `AGENTS.md`.

## Move evidence

#6623 moved `internal/query/workitem/factschema_decode.go` here by rename
plus export, folded in `logWorkItemEvidenceDecodeDrop` from
`internal/query/workitem/evidence.go`, deleted the
`internal/query/incident/store/decode_workitem.go` fork, and repointed all
consumers with no logic change.

No-Regression Evidence: `go test ./internal/query/...` and
`./internal/payloadusage/...` green before and after; the
payload-usage-manifest gate still attributes all 9 `work_item.*` kinds with
identical field sets (only the `decode_func` keys change); `factschema-diff`
reports no contract change. A wiring mutation (force `DecodeRecord` to
return an error) fails the leaf, `workitem`, and `incident/store` suites
RED, proving both consumers execute this code; `TestDerefBool` pins the
deref helper at the leaf.

No-Observability-Change: the drop-log helper keeps byte-identical messages
and attributes; this package adds no metric, span, route, or log of its own.
Whether a drop is logged remains each read path's decision to call the
helper.
