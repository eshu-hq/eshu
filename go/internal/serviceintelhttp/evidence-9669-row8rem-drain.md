<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-8 remainder drain evidence (serviceintelhttp)

No-Regression Evidence (#9669): `go/internal/serviceintelhttp` off the
in-tree `query` root — `handler.go` re-points the dossier builder to
`entity.Handler` (the `BuildServiceStoryEnvelope` seam stays a method
call), the selector to `service.WorkloadSelector`, and envelopes/params
helpers to `querycontract`; `incident_source.go` re-points truth
envelopes, profiles, and `BuildTruthEnvelope` to `querycontract`;
`supply_chain_source.go` re-points the five aggregate symbols to
`supply/chain/impact`; the three in-package test files follow the same
map (`EnvelopeMIMEType`/`ErrorCode*` and `IntVal`/`StringVal` to
`querycontract`, `AggregateCount`/`InventoryDimension`/`InventoryRow` to
`impact`); new `drain_test.go` pins zero root-query imports (including
tests). Two test-only blank imports (`query/incident`,
`query/contract`) restore the init-time capability registration the
removed root import used to provide transitively — production
registration is unchanged via the wiring binary. `go build ./...` and
`go vet ./...` clean; package tests green including the new
`TestQueryRootDrained`. Import-only retarget plus qualifier renames,
zero behavior, route, or call-graph change; JSON tags unchanged.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
