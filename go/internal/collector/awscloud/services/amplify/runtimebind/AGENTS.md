# AGENTS.md - internal/collector/awscloud/services/amplify/runtimebind guidance

## Read First

1. `README.md` - package purpose and invariants.
2. `bind.go` - the `awsruntime.Register` call.
3. `bind_test.go` - the registration-resolves proof.
4. `../README.md` - Amplify scanner contract.
5. `../../../awsruntime/README.md` - registry and runtime surface.
6. `docs/public/guides/collector-authoring.md` - AWS scanner registration
   pattern.

## Invariants

- Register exactly once from `init()` for `awscloud.ServiceAmplify`.
- Leave `RequiresRedactionKey` unset: the Amplify SDK adapter drops every
  secret-bearing field at the boundary, so no redaction key is needed.
- Do not load AWS config, acquire credentials, or construct SDK clients at init
  time; the builder does that per claim from `ScannerDeps`.
- Keep this package free of Amplify domain types, API calls, and fact emission.

## Common Changes

- This package rarely changes. Update it only when the scanner's construction
  dependencies change (for example a new required `ScannerDeps` field).

## What Not To Change Without An ADR

- Do not register a second scanner or a second service_kind here.
- Do not move SDK or scanner logic into this binding.
