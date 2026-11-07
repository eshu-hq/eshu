<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-4 drain evidence

No-Regression Evidence (#9669): `go/cmd/reducer` off in-tree `query`
imports — 17 files re-point at `querycontract` ports (plus the owning
`semanticsearch` leaf); full `go test ./cmd/reducer/` green including
13 new drain tests, `go vet` clean. Import-only retarget, zero
behavior or call-graph change.

No-Observability-Change (#9669): no telemetry added, removed, or
renamed; no new spans, metrics, or log paths.
