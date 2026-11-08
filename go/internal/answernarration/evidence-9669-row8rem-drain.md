<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-8 remainder drain evidence (answernarration)

No-Regression Evidence (#9669): `go/internal/answernarration` off the
in-tree `query` root — `types.go` and `validator.go` re-point at the
canonical leaf homes per the arbiter ruling (`AnswerPacket` to
`querycontract/answer`, `AnswerTruthClass` and the six truth-class consts
to `querycontract`); `validator_test.go` follows the same map; new
`drain_test.go` pins zero root-query imports (including tests).
`go build ./...` and `go vet ./...` clean; package tests green including
the new `TestQueryRootDrained`. Import-only retarget plus qualifier
renames, zero behavior or call-graph change; struct shapes and JSON tags
unchanged.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
