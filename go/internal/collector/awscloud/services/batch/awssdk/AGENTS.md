# AGENTS.md - services/batch/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - pagination, describe batching, and telemetry wiring.
3. `mapping.go` - SDK-to-scanner record mapping.
4. `exclusion_test.go` - the metadata-only read-surface guard.
5. `../README.md` - Batch scanner contract.

## Invariants

- Keep the `apiClient` interface List/Describe only. Never add SubmitJob,
  CancelJob, TerminateJob, RegisterJobDefinition, or any Create/Update/Delete
  operation. `exclusion_test.go` enforces this; do not weaken it.
- Never read `ContainerProperties.Command` or `JobDefinition.Parameters`. The
  scanner-owned `Container` and `JobDefinition` types do not declare those
  fields, so a leak would not compile.
- Never carry `FairsharePolicy` or `QuotaSharePolicy` state out of
  `mapSchedulingPolicy`.
- Keep recent-job listing bounded by `recentJobsPerStatus` and scoped to active
  states. Do not list terminal SUCCEEDED/FAILED history.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters. Keep ARNs, env values, secrets, and tags out of
  metric labels.

## Common Changes

- Add a new Batch read by extending `apiClient`, paginating in `client.go`, and
  mapping the response in `mapping.go`. Add the field to the scanner-owned type
  in the parent package first.
- Extend pagination or chunk sizes only with a performance note in
  `README.md`.

## What Not To Change Without An ADR

- Do not add a mutation or job-control API to `apiClient`.
- Do not move redaction into this package; values are redacted by the scanner.
- Do not add credential loading or STS calls here.
