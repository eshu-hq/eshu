# prod-freshness-service-changed-since — production validation

Capability: `freshness.service_changed_since` (tool
`get_service_changed_since`). Production profile:
`required_runtime: deployed_services`,
`max_scope_size: service_id_since_service_generation`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded service-scope changed-since delta diffing a prior service
materialization generation's evidence snapshot set against the current
active generation's set in `service_evidence_snapshots` (ownership family
only in stage 1), with an unknown `service_id` returning `service_not_found`,
an unresolved `since_generation_id` returning `not_found`, and no current
active service generation returning an explicit `unavailable` diff instead
of fabricated zero deltas.

## Committed reproducible evidence

**Handler contract, required-parameter validation, and not-found/unavailable
states** — `go/internal/query/freshness_service_changed_since_test.go`:
`TestServiceChangedSinceUnchangedProducesNoFalseDeltas`,
`TestServiceChangedSinceUnknownServiceNotFound`,
`TestServiceChangedSinceUnavailableWhenNoActiveGeneration`,
`TestServiceChangedSinceUnknownPriorGenerationNotFound`,
`TestServiceChangedSinceRequiresServiceID`, and
`TestServiceChangedSinceRequiresSinceReference`. Reproduce:

```bash
cd go && go test ./internal/query -run TestServiceChangedSince -count=1
```

## Notes

No private data: cited tests use synthetic service/generation fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
