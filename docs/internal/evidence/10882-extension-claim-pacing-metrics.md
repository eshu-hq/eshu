# Claim-grant legs record claim-pacing latch transitions (#10882)

The replay matrix found that the hosted extension leg exported no
`eshu_dp_workflow_claim_pacing_*` series in any scrape while the latch row
showed one engage and one release. The cause is wiring, not accounting:
`go/cmd/collector-component-extension/service.go` built its
`WorkflowControlStore` without `Instruments`, passed that store to
`store.New` (which returns it unchanged when the claim-plane variable is
unset), and used it as `ClaimedService.ControlStore`. The source-backpressure
consult then reached `recordClaimPacingTransition`, which returns early when
`Instruments` is nil.
`go/cmd/collector-cicd-run/service.go` and the generated-collector template
`go/internal/collector/entrypoints/service_template.go` built the same
instrument-less store.

All three now set `controlStore.Instruments = instruments`. No behavior
changes: the consult, the latch writes, and the gauge-hold accounting are
untouched. What each site reaches today:

- The hosted extension consults per-source backpressure at its claim grant
  whenever the `ESHU_SOURCE_BACKPRESSURE_*` thresholds are set.
- The template's generated collectors consult the same way when those
  thresholds are set; with them unset the leg never consults and records
  nothing.
- `collector-cicd-run` sets no `SourceBackpressure` on its `ClaimedService`, so
  its grant leg never calls `CheckSourceBackpressure`. Its line is hygiene: a
  later `SourceBackpressure` wiring there records the series. It records
  nothing today.

Observability Evidence: `eshu_dp_workflow_claim_pacing_engaged_total{domain}`
and `eshu_dp_workflow_claim_pacing_active{domain}` now follow latch
transitions made by the hosted extension's grant leg and by template
collectors with source backpressure enabled.
`TestBuildClaimedServiceRecordsClaimPacingLatchTransitions` builds the
extension's claim store through `buildClaimedService`, drives one engage and
one release through `ControlStore.CheckSourceBackpressure`, and asserts
engaged_total 1 and active 1, then active 0 with engaged_total still 1. It
failed on `origin/main` (`engaged_total{domain=vuln-intel} = 0, want 1`).
`TestBuildClaimedServiceAssignsInstrumentsToClaimControlStore` pins the field
assignment in `collector-cicd-run`, not a series. The
`TestGenerateClaimedCollectorEntrypoint` pins are substring checks on the
rendered template text. Both fail with the wiring line removed.

No-Regression Evidence: no query, lock, claim, or lease path changed; the
only change is a field assignment at process start.
