# Component activation config agent guide

## Read first

1. `config.go` — the `Config`/`RuntimeConfig` shape and `ParseConfig`'s
   validation rules.
2. `config_test.go` — the parse/validation contract every consumer depends
   on.
3. `../component_activation_config.go` — the write side: constructs a
   `Config` from a loaded component manifest and activation.
4. `../component_extension_scheduler.go` — the planning side: calls
   `ParseConfig` and builds workflow rows from the result. This file is the
   pending target of the `componentextensionplanner` extraction (#6057);
   when it moves, update every mention of it in this doc trio to the new
   package path — the ownership and invariants below do not change.
5. `../pagerduty_service.go` and `../governance_audit.go` — the two
   unrelated-provider read sites this package exists to serve without
   forcing them to import a scheduler package.

## Ownership

This package owns the JSON shape and validation rules of the generic
component-extension activation configuration only. It does not know about
collector instances, workflow rows, scheduling, Postgres, or the component
registry. If a change needs any of those, it belongs in a caller, not here.

Four coordinator files depend on this package, and none of them owns it:

- `component_activation_config.go` (root) — write side, constructs `Config`.
- `component_extension_scheduler.go` (root) — read side, plans from a parsed
  `Config`. Scheduled to move into its own `componentextensionplanner`
  package (#6057); this package's boundary does not change when it does.
- `pagerduty_service.go` (root) — reads `ParseConfig`'s `ok`/`err` only, to
  exclude component-extension instances from PagerDuty scheduling.
- `governance_audit.go` (root) — reads a parsed `Config`'s `ComponentID` to
  identify the component in a denied-egress audit event.

This is why the package exists here rather than inside the scheduler:
folding it into a scheduler-specific package would force
`pagerduty_service.go` and `governance_audit.go` — unrelated providers — to
import a scheduler package, the same shape #6057 forbids for
`owned_package_target_helpers.go` and `target_priority.go`.

## Invariants

- **Never import `internal/coordinator` or any coordinator child package.**
  Once the pending `componentextensionplanner` extraction lands, root will
  import that child for the planner request type; if this package imported
  back into `coordinator` or into `componentextensionplanner`, the import
  graph would cycle. The only permitted import is `internal/component`.
- Require `schema_version == "eshu.component.instance.v1"`; treat a blank
  or unrelated configuration as "not a component-extension instance"
  (`ok=false, err=nil`), never as an error.
- Require `component_id`, `component_version`, `manifest_digest`, and
  `config_handle` once `schema_version` matches.
- Require `runtime.sdk_protocol == component.CollectorSDKProtocolV1Alpha1`
  and `runtime.adapter` in `{oci, process}`.
- Normalize and validate an optional `host` claim; clear it when
  normalization yields an empty value instead of failing.

## Common changes and how to scope them

- **Add a new required or optional field to the activation configuration**
  → add the field to `Config` (or `RuntimeConfig`), add its validation to
  `ParseConfig`, add a case to `config_test.go`, then check every consumer
  (`component_activation_config.go`'s construction,
  `component_extension_scheduler.go`'s planning, `pagerduty_service.go`'s
  exclusion check, `governance_audit.go`'s audit identity) for whether it
  needs the new field. Do not add coordinator-specific behavior here even
  if only one consumer needs it — put that logic in the consumer.

## Verification

`go test ./internal/coordinator/componentactivation ./internal/coordinator -count=1`
covers this package plus every consumer.
