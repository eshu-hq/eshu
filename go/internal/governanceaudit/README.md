# Governance Audit

## Purpose

`governanceaudit` defines the audit-safe hosted governance event envelope and
aggregate readback helpers. It gives API, MCP, coordinator, semantic, extension,
and admin code one shared vocabulary for allowed and denied governance
decisions without carrying raw principals or source payloads.

## Ownership boundary

This package owns event shape validation and low-cardinality aggregation. It
does not own durable storage, API routing, MCP dispatch, telemetry emission,
policy loading, authorization decisions, or graph/query truth. Runtime owners
create events after they make a decision; storage and status packages decide
where approved events are persisted or displayed.

## Exported surface

See `doc.go` for the godoc contract.

- `Event` is the audit-safe decision envelope.
- `NormalizeEvent` trims and validates a single event.
- `Aggregate` validates events and returns status-safe counts.
- `EventType`, `ActorClass`, `ScopeClass`, and `Decision` define the stable
  low-cardinality enums.
- `Summary` and `Count` are readback shapes safe for status and MCP surfaces.

## Dependencies

Standard library only. This package intentionally has no dependency on query,
status, storage, telemetry, semantic policy, or collector packages.

## Telemetry

None. Runtime packages that create, store, or publish audit events own their
metrics, spans, and structured logs.

## Gotchas / invariants

- Error messages name only the invalid field, never the rejected value.
- Actor and scope identifiers must be hashes when present.
- Service principal and correlation fields accept only bounded tokens, not
  URLs, paths, email addresses, bearer tokens, or credential handles.
- Event types include identity, MFA, session, token, IdP config, role/grant,
  tenant-switch, sensitive-data, Ask/search, export, bootstrap, break-glass,
  and audit-read families. Ordinary reads stay in structured telemetry unless
  they cross a sensitive-data or export boundary.
- Aggregation validates every event before counting it, so unsafe rows cannot
  become status readbacks.

## Related docs

- `docs/internal/design/1900-hosted-governance-policy-model.md`
- `docs/public/operate/hosted-governance.md`
- `docs/public/reference/http-api/status-admin.md`
