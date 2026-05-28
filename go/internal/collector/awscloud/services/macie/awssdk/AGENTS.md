# AGENTS.md - internal/collector/awscloud/services/macie/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Macie SDK pagination and safe metadata mapping.
3. `telemetry.go` - per-call span and counter wiring.
4. `../scanner.go` - scanner-owned Macie fact selection.
5. `../README.md` - Macie scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.

## Invariants

- Keep Macie SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` interface limited to the eight metadata-only operations
  named in `README.md`. The reflection gate in `client_test.go` fails on any
  forbidden method, with the single documented `ListFindingsFilters` /
  `ListFindings` substring exemption.
- Persist only safe session, member, classification-job (counts only),
  allow-list, custom-data-identifier, findings-filter, and aggregate
  finding-count metadata.
- Map classification-job buckets and criteria to counts only; discard their
  contents immediately.
- Group finding statistics by severity label only.
- Map Macie's "not enabled" AccessDeniedException to a disabled session, but
  surface every other error.
- Map GetAdministratorAccount ResourceNotFoundException to an empty id, but
  surface every other error.

## Common Changes

- Add a new Macie metadata read by extending `Client` and the `apiClient`
  interface, writing a scanner or adapter test first, then mapping the SDK
  response into scanner-owned types. Add the new method to the allow-set in the
  reflection gate only after proving it is identity/count metadata.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not call GetSensitiveDataOccurrences, GetSensitiveDataOccurrencesAvailability,
  GetFindings, ListFindings, GetCustomDataIdentifier, BatchGetCustomDataIdentifiers,
  TestCustomDataIdentifier, GetAllowList, GetFindingsFilter,
  DescribeClassificationJob, DescribeBuckets, SearchResources, or any mutation
  API.
- Do not infer workload, environment, deployment, ownership, or defender posture
  truth from Macie metadata.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
