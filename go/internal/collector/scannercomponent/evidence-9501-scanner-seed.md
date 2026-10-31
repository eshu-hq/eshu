# Evidence: scanner seed subcommand carries no performance or observability change (#9501)

The seed implementation (`component/seed.go`, `component/seed_test.go`,
per-binary dispatch in `go/cmd/scanner-component-*`, fixture coverage in
`go/internal/runtime`) adds a one-shot init-time path: parse two flags,
validate against the pinned manifest, copy the running binary, marshal one
small YAML file. It adds no Cypher, no worker claims, no leases, no
batching, no concurrency, and no runtime or Compose/Helm settings.

No-Regression Evidence: baseline component package suite on clean
`origin/main` (`ac99a7edde`) passes in 3.549s; the same suite with seed
passes, including the built-binary smoke per analyzer, and the activation
contract suite passes 4/4. The only added production work runs once per seed invocation on a
cold path: one manifest lookup, one file copy, one YAML marshal. Manifest
construction is not on any query, ingest, or graph-write path. No benchmark
delta is claimed because no measured path exists to improve.

No-Observability-Change: no metric, span, log, or status field is added,
renamed, or removed. Seed failures return flag-and-defect-only error
strings to the init container caller; the stage is covered by the
`No-Observability-Change` marker in the scanner worker stdio components
row of `docs/public/observability/telemetry-coverage.md`, which names the
existing `eshu_dp_scanner_worker_*` signals.
