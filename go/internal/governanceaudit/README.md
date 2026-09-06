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
- `NormalizeEvent` trims and validates a single event on the write path; the
  four enums are closed there.
- `NormalizeStoredEvent` does the same for a row read back from storage, but
  keeps an enum value this build does not know when it is a bounded lowercase
  token (#6574).
- `UnknownEnums` names the enum fields of a stored event whose values this
  build does not know, so the store can log them once per page.
- `Aggregate` validates events with `NormalizeStoredEvent` and returns
  status-safe counts, so an unknown class is its own count bucket, as it is in
  the SQL summary.
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
- The enums are closed on write and tolerated on read. `NormalizeEvent` rejects
  a `type`, `actor_class`, `scope_class`, or `decision` outside this package's
  constants, so a producer on this build cannot emit one. `NormalizeStoredEvent`
  keeps such a value verbatim when it is 1-64 bytes of `[a-z0-9_]`, the shape
  every constant has, and rejects anything else; hash, token, reason-code, and
  `occurred_at` guards run unchanged, and the actor-identity rule applies only
  to a class this build knows. The reason is rolling upgrades: a release that
  adds a class has newer pods writing rows older pods must still list. Before
  #6574 the Postgres scanner used the write-path validator and an old pod
  answered the audit-list page with 500 until the rollout finished.
- `actor_class` is a closed enum too. `scoped_token` is a scoped-token or
  OIDC-bearer caller and `browser_session` is a cookie-authenticated dashboard
  session (#6459); both need an `ActorIDHash` or `ServicePrincipalID`, and an
  emitter that has neither records `anonymous` instead. One credential maps
  to one class on every event (#6566): `actorClassForAuth` in
  `internal/query/auth_audit.go` is the single mapping behind route denials,
  allowed reads, identity mutations, and admin recovery actions. `operator`
  is identity-bearing too and is reserved for a human asserting an identity
  through an SSO login, which carries no resolved credential; a cookie
  session acting on an identity mutation is `browser_session`, not
  `operator`, so the two populations stay apart when an operator filters by
  class. A request that carries no credential in the open posture (auth
  enforcement not configured) is `anonymous` on recovery actions, where it
  was `shared_token` with the synthetic identity before #6566. Rows written
  before #6566 still carry `operator` (identity mutations) or `shared_token`
  (recovery actions by a cookie session or by an open-posture request) and
  are not rewritten; a filter by class that spans that window includes both.
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
