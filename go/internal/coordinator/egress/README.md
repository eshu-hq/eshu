# Coordinator egress policy

## Purpose

`egress` parses the two hosted egress policy documents the coordinator
accepts as JSON environment configuration and evaluates fail-closed
allow/deny decisions from them: `CollectorPolicy` for scheduled collector
instances and `ExtensionPolicy` for component extension requests.

## Ownership boundary

This package owns policy parsing and the `Decide` evaluation. The parent
`internal/coordinator` package's `Config` holds a `CollectorEgressPolicy` and
an `ExtensionEgressPolicy` of these types, and the root methods that filter
instances by their decisions stay in the parent package because they are
methods on `coordinator.Service`:

- `collector_egress_filter.go` calls `CollectorPolicy.Decide` before a
  claimable collector work row is created.
- `component_extension_service.go` calls `ExtensionPolicy.Decide` before
  planning a component extension.
- `governance_audit.go` reads the resulting `CollectorDecision` and
  `ExtensionDecision` to decide whether a denial needs a governance audit
  event.

## Exported surface

- `CollectorPolicy`, `CollectorRule`, `CollectorDecision`,
  `ParseCollectorPolicyJSON`, and `(CollectorPolicy).Decide`.
- Collector action and reason constants: `CollectorActionAllow`,
  `CollectorActionDeny`, `CollectorReasonAllowed`, `CollectorReasonDenied`,
  `CollectorReasonMissing`, `CollectorReasonNotConfigured`.
- `ExtensionPolicy`, `ExtensionRule`, `ExtensionRequest`,
  `ExtensionDecision`, `ParseExtensionPolicyJSON`, and
  `(ExtensionPolicy).Decide`.
- Extension action and reason constants: `ExtensionActionAllow`,
  `ExtensionActionDeny`, `ExtensionReasonAllowed`, `ExtensionReasonDenied`,
  `ExtensionReasonMissing`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/scope` for `scope.CollectorKind`.
- `internal/workflow` to validate a collector kind through
  `DesiredCollectorInstance.Validate`.
- `internal/coordinator/planner/contract` to validate extension rule
  identifiers with `ValidateSafePlanKey`.

The package does not import the parent coordinator package.

## Telemetry

None. Decisions are evaluated inline during the parent coordinator's
reconcile and scheduling passes and inherit its reconcile metrics and
governance audit events.

No-Observability-Change: this move adds or renames no metric, span, log
field, status field, queue, worker, lease, or runtime setting. The parent
package's collector-egress and extension-egress audit events are unchanged.

## Gotchas / invariants

- The two policies do not default the same way: an unconfigured
  `ExtensionPolicy` denies every request (`ExtensionReasonMissing`), while an
  unconfigured `CollectorPolicy` allows every collector kind
  (`CollectorReasonNotConfigured`). Do not assume one policy's zero-value
  behavior from the other's.
- `broad` mode must not carry any collector- or extension-specific rules;
  parsing rejects the combination instead of silently ignoring the rules.
- `ExtensionRule.matches` treats a blank `InstanceID` or `CollectorKind` on
  the rule as a wildcard for that field, not as a rule that only matches a
  blank request value.
- `ParseCollectorPolicyJSON` and `ParseExtensionPolicyJSON` both return a
  zero-value, unconfigured policy for blank input rather than an error.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/collector_egress_filter.go`
- `go/internal/coordinator/component_extension_service.go`
- `go/internal/coordinator/governance_audit.go`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
