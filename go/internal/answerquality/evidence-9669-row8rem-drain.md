<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-8 remainder drain evidence (answerquality)

No-Regression Evidence (#9669): `go/internal/answerquality` off the
in-tree `query` root — `report_corpus.go` re-points truth envelopes,
freshness, and truth-class consts to `querycontract`; `report_score.go`
re-points truth classes to `querycontract`, `ClassifyAnswerTruth` and the
packet to `querycontract/answer`, and the playbook lookup to
`playbook.Lookup` (one local rename where `playbook` shadowed the package
name); `scorecard_test.go` follows the same map; new `drain_test.go` pins
zero root-query imports (including tests). `go build ./...` and
`go vet ./...` clean; package tests green including the new
`TestQueryRootDrained`. Import-only retarget plus qualifier renames, zero
behavior or call-graph change; struct shapes and JSON tags unchanged.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
