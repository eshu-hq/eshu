# AGENTS.md - services/autoscaling/runtimebind guidance

## Read First

1. `README.md` - binding purpose and invariants.
2. `bind.go` - the `awsruntime.Register` call.
3. `../README.md` - Auto Scaling scanner contract.
4. `../../../awsruntime/README.md` - registry and runtime surface.

## Invariants

- Register exactly once in `init()`. The registry panics on duplicate
  registrations.
- Wire `awscloud.ServiceAutoScaling` to the Auto Scaling scanner builder only.
- Do not set `RequiresRedactionKey`; the Auto Scaling scanner emits no redacted
  metadata.
- Do not load AWS config, acquire credentials, or construct clients at init
  time. The builder constructs the SDK client per claim from `ScannerDeps`.

## Common Changes

- None expected. This binding changes only if the scanner constructor signature
  or the service constant changes.

## What Not To Change Without An ADR

- Do not add a redaction-key requirement unless the scanner begins emitting
  redacted metadata.
- Do not move scanner or adapter logic into this package.
