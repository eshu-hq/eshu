# Auth OpenAPI Path Fragments

The OpenAPI path fragments for the browser session, CSRF-safe dashboard, and
admin identity-provider routes under `/api/v0/auth/...`.

Layout — one exported string constant per file:

- `routes.go` — `Routes`: provider discovery and the derived tenant
  sign-in posture (`GET /api/v0/auth/providers`).
- `setup.go` — `Setup`: the local-account setup flow.
- `tokens.go` — `Tokens`: session token issuance and refresh.
- `totp.go` — `TOTP`: TOTP enrollment and verification.
- `admin_reads.go` — `AdminReads`: admin-facing identity reads.
- `admin_mutations.go` — `AdminMutations`: admin-facing identity mutations.
- `admin_provider_configs.go` — `AdminProviderConfigs`: provider-config
  admin CRUD.
- `sign_in_policy.go` — `SignInPolicy`: the tenant sign-in policy.

`openapi/spec.go` concatenates all eight constants (`auth.Routes`,
`auth.Setup`, `auth.Tokens`, `auth.TOTP`, `auth.AdminReads`,
`auth.AdminMutations`, `auth.AdminProviderConfigs`, `auth.SignInPolicy`) into
the assembled document.

`routes.go` is 798 lines and carries an inline `//nolint:filelength`
exemption. It documents every provider-discovery response shape in one
string literal; per `internal/query/AGENTS.md`, each fragment is a single
string literal that must stay reviewable as one unit, so it cannot be split
across files the way ordinary Go source can. See this directory's
`AGENTS.md` for why that exemption is not a precedent for new files.

## Move evidence

These eight files moved here verbatim from the query root (Issue #6060 lane
C, #6642): `openapi_paths_auth.go` -> `routes.go`,
`openapi_paths_auth_setup.go` -> `setup.go`,
`openapi_paths_auth_tokens.go` -> `tokens.go`,
`openapi_paths_auth_totp.go` -> `totp.go`,
`openapi_paths_auth_admin_reads.go` -> `admin_reads.go`,
`openapi_paths_auth_admin_mutations.go` -> `admin_mutations.go`,
`openapi_paths_auth_admin_provider_configs.go` -> `admin_provider_configs.go`,
and `openapi_paths_auth_sign_in_policy.go` -> `sign_in_policy.go`. Only the
package clause and file names changed; the JSON each constant renders is
unchanged.
