# Component activation config agent guide

## Read first

1. `config.go` — the `Config`/`RuntimeConfig` shape and `ParseConfig`'s
   validation rules.
2. `config_test.go` — the parse/validation contract every consumer depends
   on.
3. `../component_activation_config.go` — the write side: constructs a
   `Config` from a loaded component manifest and activation.
4. `../planner/component/extension/planner.go` — the planning side: calls
   `ParseConfig` and builds workflow rows from the result.
5. `../component_extension_service.go` — the root eligibility checks that
   precede planner calls.
6. `../pagerduty_service.go` and `../governance_audit.go` — the two
   unrelated-provider read sites this package exists to serve without
   forcing them to import a scheduler package.

## Ownership

This package owns the JSON shape and validation rules of the generic
component-extension activation configuration only. It does not know about
collector instances, workflow rows, scheduling, Postgres, or the component
registry. If a change needs any of those, it belongs in a caller, not here.

Five production files and one test file import this package; none owns it:

- `component_activation_config.go` (root) — write side, constructs `Config`.
- `component_extension_service.go` (root) — checks activation eligibility.
- `planner/component/extension/planner.go` (child) — plans from parsed `Config`.
- `pagerduty_service.go` (root) — reads `ParseConfig`'s `ok`/`err` only, to
  exclude component-extension instances from PagerDuty scheduling.
- `governance_audit.go` (root) — reads a parsed `Config`'s `ComponentID` to
  identify the component in a denied-egress audit event.
- `component_activation_config_test.go` — the sole test importer; verifies the
  root writer through this package's parser.

This is why the package exists here rather than inside
`planner/component/extension`: moving it into the planner package would force
`pagerduty_service.go` and `governance_audit.go` — unrelated providers — to
import a scheduler-specific package, the same shape #6057 forbids for
`owned_package_target_helpers.go` and `target_priority.go`. This package
landed as its own commit before the scheduler extraction, not alongside it:
it is that extraction's prerequisite (root imports the child for its
request type, so the child cannot import root back, and this type cannot
move into the scheduler-owned child), not adjacent scope.

## Invariants

- **Never import `internal/coordinator` or any coordinator child package.**
  Root already imports `planner/component/extension` for the planner request
  type; if this package imported back into `coordinator`, or into
  `planner/component/extension`, the import graph would cycle. The only
  permitted import is `internal/component`.
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
  `component_extension_service.go`'s eligibility checks,
  `planner/component/extension` planning, `pagerduty_service.go`'s
  exclusion check, `governance_audit.go`'s audit identity) for whether it
  needs the new field. Do not add coordinator-specific behavior here even
  if only one consumer needs it — put that logic in the consumer.

## Verification

`go test ./internal/coordinator/componentactivation ./internal/coordinator/planner/component/extension ./internal/coordinator -count=1`
covers this package plus every consumer.
