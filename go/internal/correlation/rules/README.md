# Correlation Rules

## Purpose

`correlation/rules` defines the declarative rule-pack schema and the
first-party rule packs Eshu ships for container, IaC, CI/CD, Terraform
config/state drift, and AWS cloud-runtime drift correlation.

## Ownership boundary

This package owns schema validation and pack constructors only. It does not
evaluate candidates, perform admission checks, render explain output, or carry
candidate data. Those responsibilities stay in `engine`, `admission`,
`explain`, and `model`.

## Exported surface

Use `go doc ./internal/correlation/rules` for the full exported list. The core
contracts are:

- Schema types in `schema.go`: `RuleKind`, `EvidenceField`,
  `EvidenceSelector`, `EvidenceRequirement`, `Rule`, and `RulePack`.
- One constructor per shipped pack, such as `DockerfileRulePack`,
  `TerraformConfigStateDriftRulePack`, and `AWSCloudRuntimeDriftRulePack`.
- Aggregators `ContainerRulePacks` and `FirstPartyRulePacks`.
- Pack name and rule name constants that reducer/query code uses as stable
  tokens.

`RulePack.Validate` is part of the runtime contract. Keep non-negative
priorities, bounded match counts, required selectors, and admission thresholds
inside the schema rather than relying on callers.

## Dependencies

The package depends only on the standard library. Keep it metadata-only so rule
packs can be imported by reducers, tests, and docs without pulling runtime
storage or graph packages into the correlation schema.

## Telemetry

This package emits no telemetry. Reducer handlers and correlation result
publishers emit counters using pack/rule names defined here.

## Gotchas / invariants

- Engine ordering depends on `(Priority, Name)`, so renaming a rule or changing
  priority can alter deterministic explain output.
- `ContainerRulePacks` intentionally excludes Terragrunt, Ansible, Terraform
  config/state drift, and AWS runtime drift; `FirstPartyRulePacks` includes all
  shipped packs.
- Pack constructors must return fresh values. Do not share mutable slices
  between callers.
- Adding a pack requires schema tests plus downstream reducer/query/docs updates
  for any new candidate source or visible result family.

## Focused tests

```bash
go test ./internal/correlation/rules -count=1
go doc ./internal/correlation/rules
```

## Related docs

- `docs/public/reference/relationship-mapping.md`
- `go/internal/correlation/engine/README.md`
- `go/internal/correlation/admission/README.md`
