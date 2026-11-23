# NVD feed-transport emitter no-regression evidence (#11287)

No-Regression Evidence (#11287): the extension host now counts NVD window
claims, durations, cursor advances, lag positions, and sha-mismatch failures
under the closed transport labels. Baseline: no production emitter existed
(only test calls in `nvd_window_transport_test.go`; zero feed-labeled series
possible on any host). After: RED-first unit proof green
(`TestNVDWindowTransportReadsStampedPayload`,
`TestNVDWindowClaimCountsTransportsSeparately`,
`TestPendingNVDWindowClaimsEvictsOldestAtCap`), full `extensionhost`
(0.9s) / `telemetry` (2.6s) / `cmd/collector-component-extension` (0.8s)
suites green, `go vet` on touched packages clean, `git diff --check` clean,
`gofumpt` clean, telemetry-coverage verifier agrees. Backend: none (no
storage, queue, lease, or schema change; unit-level Go proof). Input shape:
table-driven label-fold cases (feed/api/absent/non-string/case-variant
stamps) plus the touched package suites. Row counts: none (no DB rows
touched). Why safe: metrics-only addition on an already-traversed
claim-outcome path; closed label sets so satellite text never becomes a
label; nil-safe instruments; absent stamp reads unknown, never feed; the
256-entry stash mirrors `maxPendingClaimOutcomes` oldest-eviction.

Observability Evidence (#11287): the emitted series are
`eshu_dp_vulnerability_nvd_window_claims_total{transport,outcome}`,
`eshu_dp_vulnerability_nvd_window_claim_duration_seconds{transport,outcome}`,
and `eshu_dp_vulnerability_nvd_window_cursor_advance_total{transport,outcome}`,
queryable per the runbook check
`sum by (transport) (rate(eshu_dp_vulnerability_nvd_window_claims_total[5m]))`;
a `transport="feed"` series appears after feed-served windows. Before the
satellite stamping pin lands, every window reads `unknown`. The feed age,
guard-trip, deadline-short, and meta-unavailable signals still have no
emitter until the satellite stamps their keys.
