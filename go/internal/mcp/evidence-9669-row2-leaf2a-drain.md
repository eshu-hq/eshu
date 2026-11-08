<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-2 leaf-2a drain evidence

No-Regression Evidence (#9669): `go/internal/mcp` Groups A/D/B off the
in-tree `query` root — 6 files re-point at `querycontract` (`answer` for
`AnswerMetadata`, `contentread` for `ContentSearchMaxOffset`):
`dispatch_budget.go`, `dispatch_calls.go`, `dispatch_envelope.go`,
`run_readonly.go`, `summaries.go`, `summaries_families.go`. Every retarget
is alias-identical (`query.ResponseEnvelope` is
`querycontract.ResponseEnvelope`, helpers forward to the same
`rowvalue` implementations); no struct or tag edits, `RunReadOnlyTool`
return type stays type-identical, zero transport change. Group C
(`router/access.go`, `transport_auth.go`) and all 99 test files untouched.
`TestQueryRootDrained` pins the cut: the only non-test importers of
`go/internal/query` under `go/internal/mcp` are the two Group C files.
`go build ./...` and `go vet ./...` clean;
`go test ./internal/mcp/... ./internal/query/... ./internal/ask/... ./internal/replay/...`
green; tool census unchanged at 171; importer census re-measured 107 (8+99)
to 101 (2+99) at this HEAD.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
