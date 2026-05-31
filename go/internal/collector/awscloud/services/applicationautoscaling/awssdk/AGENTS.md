# AGENTS.md - applicationautoscaling/awssdk guidance

## Read First

1. `README.md` - adapter purpose, read surface, and behavior.
2. `client.go` - the `apiClient` interface, namespace fan-out, pagination, and
   telemetry wrapper.
3. `mapping.go` - SDK-to-scanner type mapping and throttle warning construction.
4. `../README.md` - parent scanner contract.

## Invariants

- The `apiClient` interface lists ONLY Describe read operations. Adding any
  Register/Deregister/Put/Delete/scaling-action method is forbidden and breaks
  `exclusion_test.go`.
- Drop step-scaling and target-tracking configuration bodies during mapping;
  keep only bound CloudWatch alarm ARNs.
- Page every Describe call to exhaustion via `NextToken`.
- Classify throttling with the shared smithy `APIError` check; record a
  non-fatal warning and continue rather than failing the scan. Do not retry
  inside the adapter.
- Wrap every API call in `recordAPICall` for the shared pagination span and
  API-call/throttle counters.
- Keep every Go file under 500 lines.

## Verification

```
go test ./internal/collector/awscloud/services/applicationautoscaling/awssdk/... -count=1
golangci-lint run ./internal/collector/awscloud/services/applicationautoscaling/awssdk/...
```
