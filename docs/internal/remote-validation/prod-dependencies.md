# prod-dependencies — production validation

Capability: `dependencies.list` (package-native dependency inventory; no
dedicated MCP tool name, reached through the HTTP dependencies route).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_package_and_ecosystem_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded forward and reverse package dependency inventory anchored on
`Package.normalized_name`, with deterministic keyset paging so repeated pages
neither skip nor duplicate rows.

## Committed reproducible evidence

**Handler contract, direction, and keyset paging** —
`go/internal/query/dependencies_test.go`:
`TestDependenciesDefaultsToForwardWithDefaultLimit`,
`TestDependenciesForwardAnchorsByPackageAndEcosystem`,
`TestDependenciesReverseAnchorsOnTargetPackage`,
`TestDependenciesTruncatesAndEmitsKeysetCursor`, and
`TestDependenciesForwardCursorThreadsKeysetParams`. Reproduce:

```bash
cd go && go test ./internal/query -run TestDependencies -count=1
```

**Backend-unavailable honesty** —
`go/internal/query/dependencies_test.go`:
`TestDependenciesBackendUnavailableWhenGraphMissing` proves the handler
reports an explicit unavailable state rather than a false-empty result when
the graph backend is absent.

## Notes

No private data: cited tests use synthetic `Package` fixtures; no production
credentials or deployment-specific values appear in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
