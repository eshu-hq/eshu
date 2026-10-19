# #8473 pacing failure-story proofs (pacing half)

Lane branch `test-8473-failure-proofs`. Harness-first live-Postgres proofs for
claim pacing, each asserting its operator signal. #8470 already proves
depth-loss fail-open on the depth leg and the basic stuck
engage/overdue/release cycle; this lane adds only the gaps. Quota-floor and
clock-skew proofs stay deferred: quota code lives in unmerged PR #8557.

Proofs (all in
`go/internal/storage/postgres/workflow_control_claim_pacing_failure_live_test.go`,
throwaway Postgres 18.6, `ESHU_POSTGRES_DSN` on port 55443):

| Proof | RED (mutation) | GREEN |
|---|---|---|
| `TestWorkflowClaimPacingFailsOpenOnLatchQueryFailure` | latch table renamed away; depth read still succeeds over a pacing backlog | error names `workflow_claim_pacing`, verdict unpaced, no stuck |
| `TestWorkflowClaimPacingStuckHoldsMidBandAndReleasesBelowWatermark` | overdue engagement with depth walked 60→30 (mid-band) then →5 (below low) | mid-band overdue stays paced+stuck with the grant delay; below-low releases |
| `TestWorkflowClaimPacingStuckEmitsOperatorSignals` | wedged engagement held overdue with a test-local meter wired | `engaged_total`=1, `stuck_total`=2, `active` net 0, 3 delay samples at the grant delay |

No-Regression Evidence (#8473): this lane changes no production code —
tests, evidence docs, and generated readiness enrollment only. Baseline and
after: `go build` and `go vet ./...` on the touched packages are clean, and
the pacing family (sibling `workflow_control_claim_pacing_live_test.go` plus
the three proofs below) passes live on Postgres 18.6. No query, index,
batch, worker, lease, or concurrency knob changes.

No-Observability-Change (#8473): this lane adds no route, graph write,
queue, metric instrument, metric label, span, log key, or runtime knob.
Pacing operator signals are unchanged; the new stuck-signal proof only reads
the existing `eshu_dp_workflow_claim_pacing_*` series through a test-local
reader.
