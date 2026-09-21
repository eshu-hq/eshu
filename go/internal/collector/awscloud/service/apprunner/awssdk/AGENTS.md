# AGENTS.md - services/apprunner/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - NextToken pagination, describe enrichment, and telemetry
   wiring.
3. `mapping.go` - SDK-to-scanner record mapping.
4. `exclusion_test.go` - the metadata-only read-surface guard.
5. `../README.md` - App Runner scanner contract.

## Invariants

- Keep the `apiClient` interface List/Describe only. Never add CreateService,
  DeleteService, UpdateService, PauseService, ResumeService, StartDeployment,
  DeleteConnection, AssociateCustomDomain, or any Create/Update/Delete
  operation. `exclusion_test.go` enforces this; do not weaken it.
- Never read a runtime environment-variable value. `mapService` reads
  environment-variable NAMES only and converts secrets into ARN-only
  references. The scanner-owned `Service` type does not declare a value field,
  so a leak would not compile.
- Never read a source repository credential. Map only the connection ARN and
  access role ARN.
- Drive NextToken pagination by hand for every List operation; App Runner ships
  no SDK paginators. Keep the per-resource Describe enrichment bounded to one
  call per listed resource.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters. Keep ARNs, env values, secrets, and tags out of
  metric labels.

## Common Changes

- Add a new App Runner read by extending `apiClient`, paginating in `client.go`,
  and mapping the response in `mapping.go`. Add the field to the scanner-owned
  type in the parent package first.
- Extend pagination or describe enrichment only with a performance note in
  `README.md`.

## What Not To Change Without An ADR

- Do not add a mutation or lifecycle API to `apiClient`.
- Do not read environment-variable values or source credentials here.
- Do not add credential loading or STS calls here.
