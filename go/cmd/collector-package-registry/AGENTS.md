# AGENTS.md — cmd/collector-package-registry guidance

## Read First

1. `README.md` — command purpose, config, and telemetry
2. `config.go` — collector-instance selection and target parsing
3. `service.go` — claim service and Postgres wiring
4. `main.go` — hosted runtime setup
5. `internal/collector/packageregistry/packageruntime/README.md`

## Invariants

- This command is claim-aware only. It must select work from the workflow
  control plane and commit through `collector.ClaimedService`.
- Do not copy resolved credential values into desired collector config,
  workflow rows, facts, logs, metric labels, or docs.
- Keep package-registry runtime config bounded by explicit targets and limits.
  The command must not infer full-registry crawling from provider names.
- Keep provider-specific metadata fetch behavior in `packageruntime`; this
  package only maps process config into runtime config.

## Common Changes

- Add a target JSON field in `config.go`, validate it through
  `workflow.ValidatePackageRegistryCollectorConfiguration`, then map it into
  `packageruntime.TargetConfig`.
- Add command wiring tests in `config_test.go` whenever selection, credential
  env resolution, or claim-mode validation changes.

## What Not To Change Without An ADR

- Do not add non-claim polling mode here.
- Do not write facts directly from this command.
- Do not add graph writes or ownership inference in command wiring.
