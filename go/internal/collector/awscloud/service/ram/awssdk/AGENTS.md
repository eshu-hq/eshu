# AGENTS.md - services/ram/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - pagination and telemetry wiring.
3. `mapping.go` - SDK-to-scanner record mapping.
4. `exclusion_test.go` - the metadata-only read-surface guard.
5. `../README.md` - RAM scanner contract.

## Invariants

- Keep the `apiClient` interface Get/List only. Never add GetPermission (it
  returns the permission policy body) or any Create/Delete/Update,
  Associate/Disassociate, Accept/Reject, Enable/Disable, Promote/Replace,
  Tag/Untag, or SetDefaultPermissionVersion operation. `exclusion_test.go`
  enforces this; do not weaken it.
- `mapPermission` reads metadata only (name, ARN, version, type, status). The
  scanner-owned `Permission` type does not declare a policy field, so a
  policy-body leak would not compile.
- Scope every owner-aware read to resource owner SELF. Do not list shares owned
  by other accounts.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters. Keep ARNs, principal ids, and tags out of metric
  labels.

## Common Changes

- Add a new RAM read by extending `apiClient`, paginating in `client.go`, and
  mapping the response in `mapping.go`. Add the field to the scanner-owned type
  in the parent package first.
- Extend pagination only with a performance note in `README.md`.

## What Not To Change Without An ADR

- Do not add GetPermission or any mutation API to `apiClient`.
- Do not add a permission policy document body field to any mapped record.
- Do not add credential loading or STS calls here.
