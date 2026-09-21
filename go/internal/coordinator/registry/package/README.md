# Package-registry target planner

## Purpose

`packages` (directory `registry/package`) plans one workflow work item per
configured or derived package-registry target. It decodes a collector
instance's configured registry targets, derives additional targets from
owned-package dependency evidence across seven registry ecosystems, and ranks
targets by target class before building deterministic run and work-item
identities.

## Ownership boundary

This package owns the package-registry planning request (`PlanRequest`), the
planner implementation (`WorkPlanner`), target-derivation configuration
parsing, ecosystem target construction, and target-class ranking for this
collector. The parent `internal/coordinator` package keeps
`package_registry_service.go`, because its methods are on
`coordinator.Service`: it holds scheduling position, deployment-mode and
claims-enabled gating, and telemetry.

## Gotcha: package name vs. directory name

The directory is `registry/package`, but the package clause is `packages`,
because `package` is a reserved Go keyword. This mirrors the existing
`go/internal/projector/package` precedent. Because the last path segment
(`package`) does not match the package name (`packages`), `goimports` cannot
infer the right identifier or alias automatically: every importer needs an
explicit alias, for example:

```go
packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"
```

The parent coordinator's `package_registry_service.go` already imports it
this way.

## Exported surface

- `PlanRequest` carries the collector instance, observed time, plan key, and
  owned-package target inputs.
- `WorkPlanner.PlanPackageRegistryWork` is the planner entry point.
- `DerivationFromConfig` decodes the owned-package derivation settings from a
  collector instance's configuration.
- `DerivationEcosystems` normalizes a derivation's configured ecosystems,
  defaulting to npm.
- `DerivedTargetLimit` bounds how many derived targets one plan can produce.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/schedule` for shared target-class ranking, derivation
  limits, and skip-evidence helpers.
- `internal/coordinator/planner/contract` for the shared plan-key grammar.
- `internal/facts` for stable generation identities.
- `internal/collector/packageregistry` and `internal/packageidentity` for
  ecosystem normalization and package-identity construction.
- `internal/scope` and `internal/workflow` for collector, scope, and durable
  workflow contracts.

The package does not import the parent coordinator package.

## Telemetry

None. Planning runs inline during the parent coordinator's reconcile pass and
inherits its reconcile metrics, workflow rows, claim status, and
`/api/v0/index-status` surface.

No-Observability-Change: this move adds or renames no metric, span, log
field, status field, queue, worker, lease, or runtime setting.

## Gotchas / invariants

- The package clause is `packages`, not `package`; see the gotcha above
  before writing any import.
- Configured and derived targets are deduplicated by `scope_id` before any
  workflow row is returned; a duplicate configured `scope_id` fails planning.
- Owned-package derivation only supports npm, PyPI, Go modules, Maven, NuGet,
  Composer, and RubyGems; an unrecognized ecosystem is silently skipped, not
  an error.
- A derived target's package identity is normalized through
  `packageregistry.NormalizePackageIdentity`; a normalization failure skips
  that target rather than failing the plan.
- Target ordering after derivation is stable-sorted by
  `schedule.TargetClassRank`, not by discovery order.
- A configured target with no named packages plans as target class `broad`
  (a registry sweep), not `configured_direct`.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/planner/contract/README.md`
- `go/internal/projector/package/README.md` (the directory-name-vs-package-name
  precedent)
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
