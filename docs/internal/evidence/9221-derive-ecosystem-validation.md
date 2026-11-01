# Derive ecosystem validation per section (#9221)

Change: `ValidateVulnerabilityIntelligenceDerivationConfiguration` and the
coordinator's `LoadConfig` check the `ecosystems` list of each derive section
against that section's own set. `derive_from_owned_packages` accepts the 11 OSV
language names; `derive_from_installed_evidence` accepts those plus alma,
alpine, debian, rocky, and ubuntu. The coordinator startup refusal now names the
collector instance.

The performance gate marks every non-test Go file under `go/internal/workflow`
as hot by location, so it flags `vulnerability_intelligence_config.go`. This
change does not touch the derive limit, the ceiling, or any query. It only adds
a membership check over at most 16 names.

No-Regression Evidence: the validator runs once per collector instance when the
coordinator loads its configuration, never in a claim, lease, queue, or graph
write path. A throwaway benchmark of
`ValidateVulnerabilityIntelligenceDerivationConfiguration` on one document with
both derive sections enabled (11 owned names, 6 installed names, `target_limit`
1500), run three times at 200,000 iterations on Go 1.26.10 (darwin, arm64
host), gave:

| | ns/op | B/op | allocs/op |
|---|---|---|---|
| Base `aaf386a87a` | 2,391 to 2,463 | 1,648 | 39 |
| This change | 2,775 to 2,871 | 2,176 | 41 |

The cost is about 0.4 microseconds and two allocations more per validation.
Absolute timings move with host load (a reviewer rerun on a busier host read
about 1 microsecond higher on both sides); the delta, bytes, and allocations
reproduced. No
backend, queue, or row counts are involved, so there are no terminal counts to
report. The benchmark file was deleted after the run and is not committed.

No-Observability-Change: no metric, span, or log key is added or removed. The
refusal travels the existing path: `LoadConfig` returns the error and
`cmd/workflow-coordinator` logs `workflow coordinator failed` with it. The error
text gains the accepted set and the collector instance id, which is what an
operator needs to find the offending instance at startup.

Why it is safe: a configuration that lists a distro name under
`derive_from_owned_packages` already derived nothing, because no parser emits a
distro name as `package_manager`. It now fails at startup instead of loading,
which is the intended behavior change and is documented in
`docs/public/reference/environment-collectors.md`.
