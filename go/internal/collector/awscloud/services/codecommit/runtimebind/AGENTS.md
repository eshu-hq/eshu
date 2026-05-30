# AGENTS.md - internal/collector/awscloud/services/codecommit/runtimebind guidance

## Read First

1. `README.md` - what this binding registers and why.
2. `bind.go` - the single `awsruntime.Register` call.
3. `../README.md` - the scanner contract being registered.

## Invariants

- Register exactly once from `init()`, wiring `awscloud.ServiceCodeCommit` to the
  CodeCommit scanner builder.
- The builder constructs the SDK adapter per claim from `ScannerDeps`; do no AWS
  config or credential work at init time.
- CodeCommit persists no secret-shaped field, so leave `RequiresRedactionKey`
  unset. If a future change adds a redacted field, set it here and add the
  `d.RedactionKey.IsZero()` guard, per the collector-authoring guide.
- The matching blank import in
  `internal/collector/awscloud/awsruntime/bindings/bindings.go` must stay in
  alphabetical order; the derived supported-service guard in
  `cmd/collector-aws-cloud` fails if the runtimebind directory and the import
  set disagree.

## Common Changes

- None expected beyond the one-time registration. Scanner behavior changes go in
  the parent package and its `awssdk` adapter.

## What Not To Change Without An ADR

- Do not move registration out of `init()` or add a central dispatch switch.
- Do not add AWS API, domain, or fact logic here.
