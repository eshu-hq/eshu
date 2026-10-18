# Evidence: ifa/materializededges importer migration (#6626 batch 5)

## What changed

`go/internal/ifa/materializededges` call sites move from three reducer-root
forwarders to the owning subpackages with identical callees —
`reducer.ExtractAllCodeRelationshipRows` to
`codecall.ExtractAllRelationshipRows`, `reducer.ExtractCodeownersOwnershipEdgeRowsWithQuarantine`
to `owners.ExtractEdgeRowsWithQuarantine`, `reducer.ExtractRepoDependencyIntentRows`
to `crossrepo.ExtractRepoDependencyIntentRows` — and the three root
forwarders are deleted (`compat_correlation.go`, `compat_decode.go`,
`compat_projection.go`).

## Why no regression

No-Regression Evidence: no signature, envelope, row, or ordering change:
every deleted function body was a direct passthrough to the same callee
the call sites now name.
Proof on this head: `go build ./...` exit 0;
`go test -count=1 ./internal/ifa/materializededges/... ./internal/reducer/...`
green (including `-race`);
`scripts/test-verify-golden-corpus-gate.sh` pass (B-7 green, B-12 snapshot
byte-identical); `ci-gates validate --drift` pass;
`test-verify-telemetry-coverage.sh` 49/49. A repo-wide search for the three
deleted spellings returns zero hits in non-documentation sources.

## Observability

Observability Evidence: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. The
materialized-edge guards keep reporting through the existing Ifá guard
spans and counters.
