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
- `reason_code` is a bounded enum an operator filters and groups by, so the
  emitting package owns its closed set rather than passing through whatever a
  dependency reports. For bearer/token resolution denials that set is
  `expired`, `wrong_audience`, `unknown_issuer`, `bad_signature`, `malformed`,
  `no_grants`, `jwks_fetch_failure`, and `grant_resolution_unavailable`,
  enforced by `auditableBearerDenialReasons` in
  `internal/query/auth_audit.go`; an outcome outside it audits as
  `authentication_required`. Widening the set means adding it there too,
  otherwise a new denial kind silently reports as the generic reason. The
  distinctions matter operationally: a spike of `bad_signature` is a different
  security signal from a spike of `expired`.
- Not every failed authentication is a denial. `grant_resolution_unavailable`
  and `jwks_fetch_failure` mean a dependency could not answer, so they record
  `DecisionUnavailable`, matching the interactive OIDC and GitHub login paths.
  Recording them as denials would make a grant-store or IdP outage read as every
  authenticating subject being refused on the merits — and `no_grants` in
  particular asserts a subject holds no entitlements, which is a false statement
  about a person when the store was simply unreachable.

## Related docs

- `docs/internal/design/1900-hosted-governance-policy-model.md`
- `docs/public/operate/hosted-governance.md`
- `docs/public/reference/http-api/status-admin.md`
