<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-8 remainder drain evidence (reportbundle)

No-Regression Evidence (#9669): `go/internal/reportbundle` off the
in-tree `query` root — `capture.go` and `types.go` re-point
`ResponseEnvelope`, `ErrorEnvelope`, and `TruthEnvelope` to
`querycontract`; seven in-package test files follow the same map
(`GraphBackendNornicDB` and profiles included); the
`query.TestServiceStory...` comment reference in
`redaction_egress_test.go` is re-pointed at its canonical home
(`entity.TestServiceStoryAmbiguousEnvelopeCarriesSelectorInMessage` in
`go/internal/query/entity/service_workload_resolution_test.go`), as is
the sibling producing-sentence reference; new `drain_test.go` pins zero
root-query imports (including tests). `go build ./...` and
`go vet ./...` clean; package tests green including the new
`TestQueryRootDrained`. Import-only retarget plus qualifier renames, zero
behavior or call-graph change; bundle schema, JSON tags, and redaction
rules unchanged.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
