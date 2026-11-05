# Collector Synth Zero

## Purpose

This package owns the zero-fact synthetic emitter for the #8877 enabled
window: a stdio satellite that answers every host claim with an unchanged
result carrying one complete status row and zero facts. The window uses it to
prove the enabled claim path completes with zero fact, reducer, and graph
deltas before any real source is cut over. See `doc.go` for the full contract.

## Ownership boundary

Owns the emitter binary, the synthetic component manifest, and the authored
window artifacts in this directory (`runtime-config.yaml`,
`deployment.yaml`, `networkpolicy.yaml`, `egress-policy.json`,
`trust-grant.json`, `Dockerfile.seed`). It does not own the worker, the
claim plane, the coordinator, or any live deployment: every YAML here is
authored, never applied, until the window ruling sequences the apply step.

## Exported surface

The directory builds one binary (`eshu-collector-synth-zero`) with no
exported Go surface: `serve` reads one `extensionhost.Request` from stdin and
writes one SDK result to stdout. Window pin tests live in
`window_pin_test.go`; the disposable-venue end-to-end lives in
`window_live_test.go`.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/collector/extensionhost` — the host `Request` shape the
  emitter decodes and echoes.
- `go/internal/component` — registry install/readback/grant primitives used
  by the pin tests.
- `go/internal/coordinator/egress` — extension egress policy parsing used by
  the pin tests.
- `github.com/eshu-hq/eshu-sdk/collector` — the SDK result envelope (same
  package the host decodes with, so the wire matches by construction).

## Telemetry

The emitter emits no metrics, spans, or logs of its own. Operator visibility
comes from the host: `extensionhost.StatusTelemetry` records the unchanged
status row per claim, and the claim-plane selection counter records the
backend. A silent stderr on success is intentional.

## Performance Evidence

Benchmark Evidence: `BenchmarkServeUnchanged` answers a representative
526-byte host request in ~10 microseconds per claim with 38 allocations
(~4.8 KiB) on linux/arm64 under go1.26.10 (10,000-op sample). The per-claim
cost is one bounded JSON decode plus one small result encode; the 4 MiB stdin
cap bounds the worst case. Input shape is fixed by the host contract, so no
claim can grow the work.

No-Regression Evidence: this package touches no existing path — the worker
claim loop, host source, planner, stores, and committer are unmodified, and
the disposable end-to-end leaves `fact_records`, `scope_generations`, and
`fact_work_items` at delta 0 with the item `completed`. The only added
per-claim cost in the window is the host's process spawn, the same price every
satellite already pays.

No-Observability-Change: the emitter emits no metric, span, or log of its
own; its per-claim unchanged status row is recorded by the host's
`eshu_dp_component_extension_status_records_total` and
`eshu_dp_component_extension_results_total`, and claim timing stays on
`eshu_dp_workflow_claim_run_duration_seconds`.

## Gotchas / invariants

- Facts are always empty. Any emitted fact would flow to the committer and
  break the zero-delta window invariant; the pin tests and the live
  end-to-end assert the empty set.
- The claim is echoed verbatim. The host terminal-fails identity mismatches
  before commit, so the emitter must not normalize, reorder, or drop fields.
- `synth_zero_fact` is data only. It has no `scope` constant, no fleet list
  entry, and no workflow reducer contract; adding any of those would pull a
  window-only kind into fleet hygiene gates.
- The declared emission family is never emitted. It exists only because the
  host builds the SDK contract from the manifest and refuses a family-less
  manifest.
- The seed image digest was pinned by window assembly run 42049436432.
  `TestWindowSeedDigestAgreesAcrossVehicle` proves the `manifest.yaml`
  artifacts image and the `deployment.yaml` container image both equal that
  dispatch-proven digest (run log and attestation subject-digest recorded in
  `image-provenance.md`); a seed rebuild re-pins the test constant with them.
- The disposable-venue end-to-end runs only with `ESHU_SYNTH_ZERO_LIVE=1`:
  `ESHU_SYNTH_ZERO_LIVE=1 go test -count=1 -timeout 12m
  ./cmd/collector-synth-zero/ -run TestWindowZeroFactEndToEnd`.
  It boots embedded Postgres, so skipping stays the default.

## Related docs

- `doc.go` — godoc contract
- `templates/collector/` — lawful source the emitter derives from
- `go/cmd/collector-component-extension/` — the worker that launches this binary
- Issue `eshu-hq/eshu#8877` — the enabled window
