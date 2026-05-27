# AGENTS.md - services/cloudtrail/awssdk guidance

## Read First

1. `README.md` - adapter contract and security boundary.
2. `client.go` - the `apiClient` interface (the SDK security boundary) and
   trail metadata mapping.
3. `selectors.go` - bounded event/insight selector summaries.
4. `lake.go` - event data store, channel, and dashboard metadata mapping.
5. `telemetry.go` - shared throttle classification and tag pagination.
6. `client_test.go` and `fake_client_test.go` -
   `TestAPIClientInterfaceExcludesEventPayloadAndMutationAPIs` is the
   SDK-side guard.
7. `../README.md` - scanner contract.

## Invariants

- The `apiClient` interface is the SDK security boundary. It must not list
  `LookupEvents`, `StartQuery`, `GetQueryResults`, `CancelQuery`,
  `DescribeQuery`, `GenerateQuery`, `ListQueries`, or any CloudTrail
  mutation API.
- `GetTrailStatus` is metadata-only. Persist only the `IsLogging` boolean
  and the latest delivery/notification error strings; never project the
  delivery time fields onto event records.
- Selector outputs are reduced to bounded counts before they leave the
  adapter. The scanner does not see selector bodies.
- Dashboard widget query bodies (`QueryStatement`) are dropped on the floor;
  only `len(Widgets)` is persisted.
- Tag pagination uses `ListTags` with a single ARN per request to match the
  CloudTrail API contract (max 20 ARNs but per-resource pagination is
  per-ARN).

## Common Changes

- When CloudTrail adds a new safe metadata field on a trail, store, channel,
  or dashboard, extend the scanner-owned domain type first, then map it
  here with a focused test.
- When a new metadata-only AWS SDK method is needed, add it to `apiClient`
  AND extend the guard test forbidden list to cover any new mutation or
  data-plane sibling.

## What Not To Change Without An ADR

- Do not add `LookupEvents` or Lake query APIs to `apiClient`.
- Do not add any mutation method to `apiClient`.
- Do not persist event payloads, selector bodies, or widget query SQL from
  any code path the adapter can reach.
