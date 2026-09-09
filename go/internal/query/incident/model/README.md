# Incident Read Model

The incident-context response contract: bounded PagerDuty incident evidence
assembled into a fixed-slot evidence path.

Layout:

- `types.go` — the `IncidentContext*` row, filter, snapshot, and response
  types, the truth-label and slot vocabularies, and the capability/limit
  bounds.
- `response.go` — `BuildIncidentContextResponse`, which applies the public
  contract to a store snapshot: every expected path slot present, missing
  and ambiguous evidence derived, answer metadata attached.

The Postgres reads behind the snapshots live in `incident/store/`, the
query text in `incident/sql/`, the HTTP surface in `incident/`. The root
`incident_alias.go` keeps every pre-move `query.Incident*` spelling working
so wiring and callers outside the family are untouched.

## Move evidence

This package moved here verbatim from the query root (`incident_context_types.go`,
`incident_context_model.go`, plus the answer-metadata helper from
`answer_metadata_alias.go` whose only caller is the response assembly);
only the package clause, the `querycontract` import, and the
`incident_`→destuttered renames (`Capability`, `MissingEdge`) changed.

No-Regression Evidence: baseline `459b83f4c` vs this branch —
`go test ./internal/query/...` passes with 0 failures (counts in the lane
handoff), and the moved response assembly is pinned by the companion
`response_test.go` which moved unchanged.

No-Observability-Change: this package emits no metric, span, or log — it is
pure response assembly. The incident route keeps
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total` via the unchanged query-surface
middleware, and the handler span name is unchanged.
