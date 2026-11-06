# Collector SBOM Shadow

## Purpose

This package owns the SBOM shadow extractor for the #8877 stdio window: a
stdio satellite that answers every host claim with a complete result
carrying the 13 envelopes parsed from the single pinned CycloneDX document
baked into the binary. The window uses it to prove the enabled claim path
carries real SBOM extraction counts before the live-read half lands.
See `doc.go` for the full godoc contract.

## Ownership boundary

Owns the extractor binary and its pinned fixture copy in `testdata/`. It
does not own the sbom document parser (`go/internal/collector/sbom/document`
owns the parse), the worker, the claim plane, or any live deployment. It
performs no source observation and makes no network calls: v1 reads only
baked-in bytes.

## Exported surface

The directory builds one binary (`eshu-collector-sbom-shadow`) with no
exported Go surface: `serve` reads one `extensionhost.Request` from stdin
and writes one SDK result to stdout. `TestPinnedFixtureMatchesSource` pins
the baked-in document byte-identical to the parser's lodash fixture.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/collector/extensionhost` — the host `Request` shape the
  extractor decodes and echoes.
- `go/internal/collector/sbom/document` — the existing CycloneDX parser
  that turns the pinned bytes into reported-confidence envelopes.
- `go/internal/facts` — the envelope shape mapped onto SDK facts.
- `github.com/eshu-hq/eshu-sdk/collector` — the SDK result envelope (same
  package the host decodes with, so the wire matches by construction).

## Telemetry

The extractor emits no metrics, spans, or logs of its own. Operator
visibility comes from the host: `extensionhost.StatusTelemetry` records the
complete status row per claim. A silent stderr on success is intentional;
every failure names counts and knob names only, never request or document
values.

## Performance Evidence

Performance Evidence: one claim costs one bounded request decode, one parse
of the 2,874-byte pinned document, and one 13-fact result encode, with stdin
capped at 4 MiB and no network, lock, queue, or shared state on the path.

Benchmark Evidence: `BenchmarkServeComplete` answers one representative host
claim in 268-271us/op on linux/arm64 under go1.26.6 (2,000-op sample, 3 runs:
268611, 269157, 268745 ns/op). The input shape is fixed by the host contract
(one claim envelope per call), so no claim can grow the work beyond the
stdin cap.

No-Regression Evidence: additive binary only; no existing package is
modified, so the worker claim loop, host source, planner, stores, and
committer are untouched. Package suite 7/7 PASS plus `-race` clean on the
same head.

No-Observability-Change: the emitter records no metric, span, or log of its
own; per-claim status rows and the real fact count land on the host's
`eshu_dp_component_extension_status_records_total`,
`eshu_dp_component_extension_results_total`, and
`eshu_dp_component_extension_reported_facts_total` (extension host row in
`telemetry-coverage-collectors.md`), with claim timing on
`eshu_dp_workflow_claim_run_duration_seconds`.

## Gotchas / invariants

- The result state is complete, never unchanged: the SDK host validator
  rejects an unchanged result that carries facts.
- The generation ID always equals the claim generation ID; the host
  requires the match.
- The pinned fixture copy must stay byte-identical to
  `go/internal/collector/sbom/document/testdata/cyclonedx_image_subject.json`;
  a re-pin is deliberate and updates the golden counts in the same commit.
- The `.invalid` provenance URI is intentional (RFC 2606): v1 resolves
  nothing over the network.

## Related docs

- `go/internal/collector/extensionhost/README.md`
- `go/internal/collector/sbom/document/README.md`
