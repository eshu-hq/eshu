# GitHub App installation-token minter

## Purpose

`internal/github/app` mints GitHub App installation tokens for collector
runtimes that cannot hold a long-lived credential. It owns the RS256 JWT
shape, the bounded in-call retry budget, and the final-versus-retryable
fault classes in one place so the repository-discovery runtime and the
security-alert runtime classify the same provider fault the same way.

## Ownership boundary

This package owns App credential validation, JWT signing, the mint request,
and retry classification only. It does not own provider alert endpoints,
fact envelopes, workflow claims, credential storage or resolution, or any
metric family: attempt and retry callbacks flow through the caller-supplied
`Observer`, and runtimes keep counting the `mint` operation on their existing
instruments.

## Exported surface

See `doc.go` for the godoc package contract. The public surface includes:

- `Config` for one mint: base URL, App ID, installation ID, private key,
  HTTP client, retry budget, clock, and observer.
- `MintInstallationToken`, which signs a fresh JWT per call
  (`iat=now-60s`, `exp=now+540s`) and posts it to
  `{base}/app/installations/{id}/access_tokens`.
- `NormalizePrivateKeyPEM`, which re-wraps single-line secret-env PEM bodies.
- `RetryableError` (`server_error`, `rate_limited`, or a bounded transport
  class), `AuthDeniedError` (401, un-signalled 403), `NotFoundError` (404),
  and `ConfigError` (validation, key, and decode failures). No error carries
  credentials, keys, tokens, or JWTs.

## Dependencies

`github.com/golang-jwt/jwt/v5` for RS256 signing and
`internal/collector/sdk` for transient-transport classification. No other
Eshu internal packages.

## Telemetry

None defined here. Callers pass an `Observer` to keep their existing
attempt and retry signals; a nil observer disables callbacks.

## Gotchas / invariants

- One mint per call: the package holds no token cache and no background
  refresh. Callers mint once per unit of work.
- A rate-limit hint longer than 60 seconds is never slept in-call; the mint
  returns a `RetryableError` with the capped `RetryAfter` instead.
- `iat=now-60s` covers 60 seconds of clock skew only. Larger skew fails
  closed as a terminal denial; operators fix the clock.
- No in-call re-mint loop exists: a 404 surfaces once as terminal, never as
  a retry storm.
