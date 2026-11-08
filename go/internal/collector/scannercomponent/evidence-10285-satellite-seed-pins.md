# Evidence: satellite seed pin registry carries no performance or observability change (#10285)

The satellite registry change (`satellite_seed.go`) adds a two-entry pin
table plus one map lookup and a small platform set compare in
`VerifySeededArtifact`, on the per-artifact admission path of the extension
worker and the coordinator planner. It adds no Cypher, no worker claims, no
leases, no batching, no concurrency knobs, and no runtime or Compose/Helm
settings.

No-Regression Evidence: `BenchmarkVerifySatelliteSeededArtifact` (go1.26.6
linux/arm64, this host) measures 56.20 ns/op pinned and 11.06 ns/op
pass-through over 100,000 iterations. Admission runs once per artifact next
to manifest fetch and image verification measured in milliseconds, so the
added work is six orders of magnitude below the surrounding path and no
hot-path statement changes shape or cardinality. Baseline package suite on
clean `origin/main` and the same suite with the registry plus new tests
both pass; with `-race` the touched packages pass.

No-Observability-Change: no metric, span, log, or status field is added,
renamed, or removed (`verify-telemetry-coverage.sh` PASS). The new strings
are fail-closed error returns for drifted, mismatched, or duplicate-platform
artifacts, surfaced to the caller, not emitted as telemetry.

## Host-platform scoping carries no performance or observability change (#9877)

`VerifySatelliteSeededArtifact` now takes the executing host platform and
compares only that slot against the registry pin instead of demanding
full-set equality. Nothing else on the startup admission path changes, and
admission still runs once per process start in `LoadConfig` /
`selectActivation`, next to manifest fetch and image verification measured
in milliseconds.

No-Regression Evidence: `BenchmarkVerifySatelliteSeededArtifact` (go1.26.6
linux/arm64, this host, 100,000 iterations x3) measures 20.3 ns/op pinned and
11.5 ns/op pass-through after the change, against 56.1 ns/op pinned and
11.4 ns/op pass-through on clean `origin/main` (`c0cc0918d9`) on the same
host: same order, the scoped check faster, six orders of magnitude below the
surrounding path. Touched package suites pass with `-count=1`, including the
new seed-stamped sibling regression test that replays the QA refusal shape.

No-Observability-Change: no metric, span, log, or status field is added,
renamed, or removed. Refusal reasons keep the defect-only shape (component
ID plus a pinned-image defect, never an image value), asserted by the
no-echo tests; the startup log envelope that carries the error is unchanged.
