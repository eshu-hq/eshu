<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-3 drain evidence

No-Regression Evidence (#9669): `go/internal/cli` off in-tree `query`
root imports — 22 files re-point at `querycontract`/`packet` ports (plus
the new owning `querycontract/packet` leaf); `go build ./...` and
`go vet ./...` clean; `go test ./internal/cli/... ./internal/query/...`
green except the pre-existing environmental `cli/demo` failure (package
untouched, needs `ESHU_DEPLOY_CHECKOUT`). Import-only retarget plus
qualifier renames, zero behavior or call-graph change; moved bodies
byte-identical, wire JSON/tags unchanged.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
