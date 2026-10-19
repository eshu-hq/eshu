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
| `TestWorkflowClaimPacingFailsOpenOnLatchQueryFailure` | pace-on-missing-latch: consult returned paced with nil error | error names `workflow_claim_pacing`, verdict unpaced, no stuck |
| `TestWorkflowClaimPacingStuckHoldsMidBandAndReleasesBelowWatermark` | release-at-high: mid-band overdue released (`Paced:false`) | mid-band overdue stays paced+stuck with the grant delay; below-low releases |
| `TestWorkflowClaimPacingStuckEmitsOperatorSignals` | stuck counter disabled: `stuck_total` empty | `engaged_total`=1, `stuck_total`=2, `active` net 0, 3 delay samples at the grant delay |

No-Regression Evidence (#8473): the lane's only production-code touch is
spelling the canonical contract types after the #6626 alias retirement
deleted the aliases while the #5385 line added new bare uses, which left
origin/main 8a8032768 unbuildable (`undefined: Intent` in two reducer
handlers, `undefined: Domain` in one wiring test). Baseline: `go build
./internal/storage/postgres/` fails on that head. After: `go build` and
`go vet ./...` on the touched packages are clean, the ladder wiring tests
(`TestHandleLadderRegistrationCarriesSiblingLookup`,
`TestHandleLadderRegistrationKeepsLegacyPassThroughWithoutLookup`) pass
unchanged, and the full pacing family (20 live proofs) passes. No query,
index, batch, worker, lease, or concurrency knob changes.

No-Observability-Change (#8473): the enabling fix changes identifier
spelling only. It adds no route, graph write, queue, metric instrument,
metric label, span, log key, or runtime knob. Pacing operator signals are
unchanged; the new stuck-signal proof only reads the existing
`eshu_dp_workflow_claim_pacing_*` series through a test-local reader.
