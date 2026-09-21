# AGENTS.md - services/appsync/runtimebind guidance

## Read First

1. `README.md` - one-binding contract and ownership boundary.
2. `bind.go` - the actual registration.
3. `../README.md` - AppSync scanner contract.
4. `../../../awsruntime/README.md` - awsruntime registry and runtime surface.

## Invariants

- Register exactly once from `init()` with `awscloud.ServiceAppSync`.
- Keep the builder a constructor call with no redaction-key check. AppSync needs
  no `RedactionKey`; leave `RequiresRedactionKey` unset (false) in the
  registration. The command derives the `ESHU_AWS_REDACTION_KEY` requirement from
  that flag, so setting it would force a key on AppSync-only targets that do not
  need one.
- Do not load AWS configuration or build SDK clients at init time. Builders
  construct clients per claim from `ScannerDeps`.
- Do not validate or transform claims here. Validation belongs to awsruntime
  and the scanner.
- Do not import anything else from `internal/collector/awscloud/services`.
  Cross-service knowledge belongs upstream.

## Common Changes

- Update the builder body only when the AppSync scanner constructor signature
  changes. Keep the change scoped to the constructor call.

## What Not To Change Without An ADR

- Do not move the `Register` call out of `init()`.
- Do not introduce side effects (network, file IO, config parsing) at package
  load time.
