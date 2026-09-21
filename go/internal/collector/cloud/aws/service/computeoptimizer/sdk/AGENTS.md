# AGENTS.md - computeoptimizer/awssdk guidance

## Read First

1. `README.md` - adapter purpose, read surface, and behavior.
2. `client.go` - SDK client, pagination, opt-in/throttle classification, and
   telemetry.
3. `mapper.go` - SDK-to-scanner type mapping.
4. `../README.md` - scanner contract and resource_id shapes.

## Invariants

- The `apiClient` interface lists ONLY Compute Optimizer Get reads. Never add an
  enrollment mutation (`UpdateEnrollmentStatus`), a recommendation-preference
  mutation (`PutRecommendationPreferences`,
  `DeleteRecommendationPreferences`), an export start, or any other non-Get
  method. `exclusion_test.go` enforces this at build time.
- Never map CloudWatch utilization metric data points or customer cost data
  points into scanner types. Keep only the savings-opportunity percentage.
- Page every get API to exhaustion on `NextToken`.
- Treat `OptInRequiredException` (and an access-denied error naming the opt-in
  requirement) as "not enrolled": return an empty snapshot, not an error.
- Do not retry inside the adapter. Classify throttle errors through the shared
  classifier and record them on the throttle counter.
- Wrap every API call in `recordAPICall` so the shared pagination span and
  API-call/throttle counters stay consistent with other AWS scanners.
- Keep ARNs, names, findings, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new metadata field by extending the scanner-owned type in the parent
  package, writing a focused adapter test first, then mapping it here. If the
  field carries metric data points or cost values, do not map it.
- Add a new paginated read only when the parent scanner needs the metadata and
  the API is a control-plane Get read.

## What Not To Change Without An ADR

- Do not add a mutation, enrollment, or export API to the adapter surface.
- Do not add retry logic inside the adapter.
- Do not move identity keying or graph-edge logic into this package.
