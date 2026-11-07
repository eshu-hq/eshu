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
