# Status OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's status, admin, and operator
surfaces — the broadest leaf in the `openapi/paths` tree.

Layout:

- `routes.go` — `Routes`: `GET /api/v0/status/pipeline`,
  `GET /api/v0/collectors`, `GET /api/v0/status/collectors`,
  `GET /api/v0/ingesters`, `GET /api/v0/ingesters/{ingester}`,
  `GET /api/v0/status/ingesters`, `GET /api/v0/status/ingesters/{ingester}`,
  `GET /api/v0/index-status`, `GET /api/v0/status/index`, and
  `GET /api/v0/openapi.json`.
- `compare.go` — `Compare`: `POST /api/v0/compare/environments`.
- `admin.go` — `Admin`: the `/api/v0/admin/*` mutation and query routes
  (backfill, dead-letter, decisions, input-invalid-facts, recover-generations,
  refinalize, reindex, replay, replay-events, shared-projection tuning
  report, skip, work-items).
- `answer_narration.go` — `AnswerNarration`:
  `GET /api/v0/status/answer-narration`.
- `collector_readiness.go` — `CollectorReadiness`:
  `GET /api/v0/collector-readiness` and
  `GET /api/v0/status/collector-readiness`.
- `collector_extraction_readiness.go` — `CollectorExtractionReadiness`:
  `GET /api/v0/collector-extraction-readiness` and
  `GET /api/v0/collector-extraction-readiness/{family}`. Also declares the
  unexported `collectorExtractionReadinessSchema`, the inline per-criterion
  schema object reused by both the list and drilldown responses.
- `fact_schema_version.go` — `FactSchemaVersion`:
  `GET /api/v0/fact-schema-versions` and
  `GET /api/v0/fact-schema-versions/{fact_kind}`.
- `governance.go` — `Governance`: `GET /api/v0/status/governance`.
- `hosted_readiness.go` — `HostedReadiness`:
  `GET /api/v0/status/hosted-readiness`.
- `metrics.go` — `Metrics`: `GET /api/v0/metrics/timeseries`.
- `operations.go` — `Operations`: `GET /api/v0/status/operations`.
- `operator_control_plane.go` — `OperatorControlPlane`:
  `GET /api/v0/status/operator-control-plane`.
- `semantic.go` — `Semantic`: `GET /api/v0/status/semantic-extraction`.

## routes.go and compare.go are split, not merged

`routes.go`'s constant was named `openAPIPathsStatusAndCompare` before this
move even though it declared no `/compare` route — the
`/api/v0/compare/environments` route has always lived in the separate
`compare.go` file, split out to keep both files under the 500-line cap. The
move renamed the constant to `Routes` and dropped the stale "AndCompare"
half of the name; the file split itself is unchanged and stays that way.

## Adding a fragment

A new status route gets its own file exporting one JSON string constant
(or follows `compare.go`'s precedent and splits out of `routes.go` once a
file nears the line cap), matching the shape of the existing files.
Register the constant in `openapi/spec.go`'s concatenation and add the
route to
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR. The assembled spec is a published wire contract: a fragment
added here but not wired into `spec.go` never reaches API consumers even
though the handler behind it still works.

## Move evidence

This tree is destination-only for the #6642 path split: these thirteen
constants and their route sets were confirmed against the pre-move
`openapi_paths_*.go` files (`rg -o '"/api/v0[^"]*"'`) with no route added,
removed, or reworded, apart from the `Routes` rename described above.
