# Incident

## Purpose

The incident package owns incident-management source fact kinds: operational
incidents, lifecycle events, and change records, plus incident-routing
evidence from Terraform state and PagerDuty. It exists so the facts root
stays a registry, not a family container (issue #6951).

## Ownership boundary

Source-evidence declarations only. The package owns kind constants, family
kind lists, schema-version lookups, and the `contract.Declare` family
declarations. It does not own admission, projection, or read models —
reducers and projectors consume these kinds downstream.

## Exported surface

- `IncidentContextFactKinds`, `IncidentContextSchemaVersion` — incident source kinds
- `IncidentRoutingFactKinds`, `IncidentRoutingSchemaVersion` — routing evidence kinds
- `IncidentRecordFactKind`, `IncidentRouting*FactKind`, `ChangeRecordFactKind` — kind constants
- `IncidentContextFamilyDeclaration`, `IncidentRoutingFamilyDeclaration` — registry declarations

See `doc.go` for the full contract.

## Dependencies

- `internal/facts/contract` — family declaration type
- Standard library only otherwise. Must not import `internal/facts`.

## Telemetry

None of its own. Kind declaration is compile-time data.

## Gotchas / invariants

- Kind strings are contracts: collectors emit them and the registry,
  schema-version table, and loaders match on them. Renaming a kind is a
  contract change, not a refactor.
- The schema-version table in the parent `schema_version.go` must list both
  families, or the classifier and drift guard go blind to incident kinds.

## Related docs

- `docs/internal/naming.md` (nesting rules behind the `incident/` split)
