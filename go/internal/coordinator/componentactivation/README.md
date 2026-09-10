# Component activation config

## Purpose

`componentactivation` owns the parse and validation contract for the
generic component-extension activation configuration
(`eshu.component.instance.v1`) that a claim-capable component collector
instance carries in its `Configuration` field. It exists because that
configuration shape is a genuine cross-cutting contract, not scheduler
state: root constructs and checks it, the `planner/component/extension` child
consumes it, and two unrelated providers read it, so it needs a home none of them
owns. This package landed as its own commit before the scheduler extraction
(#6057) because it is that extraction's prerequisite, not an optional
follow-up: root already imports the child for its request type, so the
child cannot import root back, and this type cannot move into the
scheduler-owned child without forcing the two unrelated providers below to
depend on a scheduler-specific package.

## Ownership boundary

This package owns the JSON shape of the activation configuration and its
validation rules: required fields, the `schema_version` gate, the supported
SDK protocol and runtime adapter values, and host-claim normalization. It
does not resolve component artifacts, read the component registry, plan
workflow rows, or touch Postgres.

Five production files import this package. Root's
`component_activation_config.go` constructs and marshals `Config` values.
`component_extension_service.go` checks activation eligibility before calling
the planner. `planner/component/extension/planner.go` turns a parsed `Config`
into deterministic workflow rows. `pagerduty_service.go` excludes component
extensions from PagerDuty scheduling, and `governance_audit.go` reads the
component identity for denied-egress audit events. The sole test importer is
`component_activation_config_test.go`, which verifies root's written JSON
through `ParseConfig`. None of these consumers owns the shared contract.

## Exported surface

- `ConfigSchema` is the required `schema_version` value
  (`eshu.component.instance.v1`).
- `Config` is the parsed activation configuration: component identity,
  manifest digest, config handle, an optional host claim, and the runtime
  binding. Root's `component_activation_config.go` constructs one directly
  when it builds a collector instance's `Configuration` JSON; every other
  consumer gets one back from `ParseConfig`.
- `RuntimeConfig` is the collector-SDK runtime binding (`sdk_protocol`,
  `adapter`) nested under `Config.Runtime`.
- `ParseConfig` parses and validates raw configuration, reporting whether it
  is a component-extension activation configuration at all
  (`ok=false, err=nil` for a blank or unrelated configuration) and, when it
  is, whether it is complete and valid (`ok=false` with a descriptive `err`
  otherwise).

See `doc.go` for the godoc contract.

## Dependencies

- `internal/component` supplies `ActivationHostClaimMetadata`, the SDK
  protocol constant, and the adapter constants this package validates
  against. This is the only dependency; the package imports no
  `internal/coordinator` symbol.

## Telemetry

This package is a pure value parser. It emits no telemetry itself; every
consumer's own telemetry (coordinator reconcile metrics, planner package
docs) covers the paths that call into it.

No-Observability-Change: this package adds no metric, span, log field,
status value, queue, worker, lease, retry, or runtime setting.

## Gotchas / invariants

- `schema_version` must equal `eshu.component.instance.v1`; a blank
  configuration or one with neither `schema_version` nor `component_id` is
  "not a component-extension instance" (`ok=false, err=nil`), not an error.
- `component_id`, `component_version`, `manifest_digest`, and
  `config_handle` are all required once `schema_version` matches; a missing
  one is a validation error, not a miss.
- `runtime.sdk_protocol` must equal `component.CollectorSDKProtocolV1Alpha1`
  and `runtime.adapter` must be `oci` or `process`.
- An optional `host` claim is normalized and validated when present; an
  empty normalized host clears the field rather than failing validation.
- This package must never import `internal/coordinator` or any of its child
  packages. If a future change to `Config` needs coordinator-only context,
  that context belongs in the caller, not here — importing coordinator here
  would reintroduce the exact import cycle this package exists to avoid.

No-Regression Evidence: `go test ./internal/coordinator/componentactivation -count=1`
covers a blank configuration, an unrelated collector configuration, a valid
configuration with component identity, a missing required field, an
unsupported `runtime.sdk_protocol`, and an unsupported `runtime.adapter`.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/planner/component/extension/README.md`
- `docs/internal/design/package-restructure.md`
