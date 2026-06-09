# AGENTS.md - internal/governanceaudit guidance

## Read first

1. `go/internal/governanceaudit/README.md` - package boundary and invariants.
2. `go/internal/governanceaudit/doc.go` - godoc contract.
3. `go/internal/governanceaudit/audit.go` - event enums, validation, and aggregation.
4. `go/internal/governanceaudit/audit_test.go` - unsafe-value and aggregation proof.

## Invariants this package enforces

- **No raw values in errors** - validation errors may name the invalid field,
  but must never echo the rejected value.
- **Bounded enums only** - event types, actor classes, scope classes, decisions,
  and reason codes must stay low-cardinality for status and metrics safety.
- **Hashes for identities** - actor, scope, and policy revision identifiers
  are hashes when present.
- **Pure helpers only** - this package does not persist events, emit telemetry,
  call policy engines, or mutate graph/query state.

## Common changes and how to scope them

- Add an event type only when a hosted governance decision needs audit readback.
  Update `audit.go`, tests, and public docs in the same PR.
- Add an actor or scope class only when it is safe to expose in status/MCP
  aggregate counts.
- If a caller needs durable storage, add the storage implementation in the
  owning package and keep this package as the validation contract.

## Failure modes and how to debug

- A validation error for `actor_identity` usually means a non-anonymous,
  non-system event omitted both `ActorIDHash` and `ServicePrincipalID`.
- A validation error for a hash field means the caller passed an identifier,
  path, URL, or malformed digest instead of a `sha256:` hash.
- A validation error for a token field means the caller passed a value that
  could be a URL, path, email address, credential handle, or raw token.

## What not to change without review

- Do not relax validation to accept URLs, paths, email addresses, or arbitrary
  strings.
- Do not add storage, telemetry, or policy evaluation to this package.
