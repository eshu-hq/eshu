# Component activation config

## Purpose

`componentactivation` owns the parse and validation contract for the
generic component-extension activation configuration
(`eshu.component.instance.v1`) that a claim-capable component collector
instance carries in its `Configuration` field. It exists because that
configuration shape is a genuine cross-cutting contract, not scheduler
state: root constructs it, the component-extension scheduler consumes it,
and two unrelated providers read it, so it needs a home neither of them
owns. This package is the prerequisite for the pending extraction of
`component_extension_scheduler.go` into its own `componentextensionplanner`
child package (tracked in #6057): once the scheduler moves, it will import
this package too, exactly as it already calls into it from root today.

## Ownership boundary

This package owns the JSON shape of the activation configuration and its
validation rules: required fields, the `schema_version` gate, the supported
SDK protocol and runtime adapter values, and host-claim normalization. It
does not resolve component artifacts, read the component registry, plan
workflow rows, or touch Postgres.

`internal/coordinator/component_activation_config.go` (root) owns
constructing a `Config` from a loaded component manifest and activation and
marshaling it into a collector instance's `Configuration` field — the write
side of this contract. `internal/coordinator/component_extension_scheduler.go`
(root) owns turning a parsed `Config` into a deterministic workflow run and
work item — the planning side. `internal/coordinator/pagerduty_service.go`
and `governance_audit.go` read a parsed `Config` for reasons that have
nothing to do with either of those: excluding a component-extension
instance from PagerDuty scheduling, and identifying the component in a
denied-egress audit event. None of those four consumers owns this contract,
which is why it lives here instead of in any of their packages.

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
- `docs/internal/design/package-restructure.md`
