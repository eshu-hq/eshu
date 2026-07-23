# prod-environment-compare — production validation

Capability: `platform_impact.environment_compare` (tool
`compare_environments`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 8000`, `max_truth_level: exact`; exact only when compared
environments are fully indexed.

## Claim validated

Environment comparison serves present snapshots from materialized runtime
instances and, when direct instance evidence is absent, inferred snapshots
from service evidence — with mixed present/inferred states kept honestly
distinct and an explicit unsupported result when evidence is truly absent
rather than a fabricated empty diff.

## Committed reproducible evidence

**Handler contract, honesty of present-vs-inferred state, and bounds** —
`go/internal/query/compare_test.go`:
`TestCompareEnvironmentsReturnsPresentSnapshotsFromMaterializedInstances`,
`TestCompareEnvironmentsReturnsInferredSnapshotsFromServiceEvidence`,
`TestCompareEnvironmentsKeepsMixedPresentAndInferredStatesHonest`,
`TestCompareEnvironmentsReturnsExplicitUnsupportedWhenEvidenceIsTrulyAbsent`,
and `TestCompareEnvironmentsBoundsResourceReadsAndReportsTruncation`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestCompareEnvironments -count=1
```

## Notes

No private data: cited tests use synthetic workload-instance and
service-evidence fixtures; no production credentials or deployment-specific
values appear in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
