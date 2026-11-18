# NVD 6h mint + 600s envelope evidence (#10446)

No-Regression Evidence: baseline `origin/main` 3e7647c17e (48h NVD mint,
60s worker lease fallback) vs this lane (6h NVD-only mint via
`nvdPlannerDefaultChunkWidth`, 600s worker fallback
`defaultComponentClaimLeaseTTL` with 20s heartbeat coherence). Unit
proof after: `go test -count=1 ./internal/coordinator/vulnerability/
./internal/vulnsource/ ./internal/collector/extensionhost/
./cmd/collector-component-extension/ ./cmd/collector-synth-zero/
./internal/envregistry/` all ok; RED-first mint and fallback tests
fail pre-fix and pass post-fix; `git diff --check` and `go vet` clean.
Input shape unchanged (same targets in, same claims out; only window
width and lease ceiling differ). Terminal counts unchanged by
construction: narrower windows tile the same instants (planner tests),
and the fallback only raises the ceiling a 546s+ paced walk must fit
under. No cursor, index, serialization, or same-cursor-parallelism
changes. NOT_CHECKED: timed live head-window walk at final settings
(owed pre-soak per the arbiter ruling).

No-Observability-Change: the 6h mint emits through the existing
`eshu_dp_vulnerability_nvd_cursor_lag_seconds` gauge and NVD window
claim outcomes; the 600s fallback emits no new signal. Over-long
fetches stay visible on
`eshu_dp_workflow_claim_dispatch_lease_remaining_seconds`,
`eshu_dp_workflow_claim_run_duration_seconds`, and `launch_failure` on
`eshu_dp_component_extension_results_total` (new coverage row in
`docs/public/observability/telemetry-coverage-collectors.md`).
