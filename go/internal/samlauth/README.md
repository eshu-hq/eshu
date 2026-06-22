# SAML Auth

## Purpose

This package holds Eshu's SAML service-provider validation helpers. It parses
IdP metadata, renders SP metadata, normalizes assertion claims, applies
assertion time-window checks, and produces replay fingerprints for the login
flow.

## Ownership boundary

`samlauth` owns protocol-adjacent validation and hash-safe claim shaping. It
does not own provider configuration storage, atomic replay reservation,
membership lookup, role/grant calculation, HTTP cookies, or browser-session
creation. External IdPs authenticate users; Eshu remains the authorization and
session owner.

## Exported surface

See `doc.go` for the godoc contract. The exported surface is intentionally
small: metadata validation and rendering, assertion claim normalization,
assertion time-window validation, and replay fingerprint construction.

## Dependencies

The package depends on `github.com/crewjam/saml` and `github.com/crewjam/saml/samlsp`
for SAML metadata and service-provider XML handling. It does not import Eshu
storage, query, telemetry, or runtime packages.

## Telemetry

This package emits no metrics, spans, or logs. Callers should record bounded
reason codes such as metadata expired, group claims missing, replay detected, or
clock skew rejected without including raw SAML values.

## Gotchas / invariants

SAML claim values may be sensitive even in tests. Use synthetic `example.test`
subjects and fake groups only. Replay fingerprints are not a replay check by
themselves; storage must reserve them through a unique constraint before
creating a session.

## Related docs

- `/docs/internal/design/3452-user-management-identity-federation.md`
- `/docs/public/reference/http-api.md`
- `/docs/public/reference/authorization-catalog.md`
