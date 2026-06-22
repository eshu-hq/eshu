# go/internal/samlauth Agent Instructions

## Read First

- `/AGENTS.md`
- `/docs/internal/agent-guide.md`
- `/docs/internal/design/3452-user-management-identity-federation.md`
- `/docs/public/reference/http-api.md`
- `/docs/public/reference/authorization-catalog.md`
- `README.md`
- `doc.go`

## Invariants

- SAML identifies an external subject; it never grants Eshu permissions by
  itself.
- Keep raw SAML assertions, NameID values, email addresses, group names,
  metadata URLs, private endpoints, and provider secrets out of logs, docs,
  issues, tests, and durable rows.
- Use maintained SAML parsing and validation primitives for protocol XML. Do
  not hand-roll assertion signature or wrapping validation.
- Replay fingerprints must be hash-only and reserved atomically by the storage
  caller before a browser session is created.
- Missing required group claims, expired metadata, bad issuer, bad audience, bad
  ACS, replay, and clock-skew failures fail closed for new logins.

## Common Changes

- Metadata validation and SP metadata rendering belong in this package.
- HTTP routes, cookies, and session creation belong in `go/internal/query`.
- Durable provider, replay, and membership/role/grant mapping belongs behind
  storage interfaces in `go/internal/storage/postgres`.
- API startup wiring belongs in `go/cmd/api`, preferably split out of
  near-limit files.

## Verification

Run focused tests after package changes:

```bash
cd go && go test ./internal/samlauth -count=1
scripts/verify-package-docs.sh
git diff --check
```
