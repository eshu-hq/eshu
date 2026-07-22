# Console First Five Minutes

This guide takes an operator from `docker compose up` to a logged-in Console
dashboard with a real identity provider connected. Every command below is
copy-pasteable — no manual SQL, no environment variables you have to guess at.
It is the Console/SSO companion to
[First Successful Run](first-successful-run.md), which covers the CLI and MCP
indexing path instead.

By the end you will have:

1. The default Docker Compose stack running.
2. The Console dev server running and pointed at that stack.
3. The generated one-time bootstrap admin credential.
4. A completed first-run setup wizard (claim, create admin, save MFA recovery
   codes).
5. A logged-in dashboard.
6. An OIDC identity provider added, tested, and enabled from
   **Admin -> Identity & Access**.

## Prerequisites

- Docker and Docker Compose.
- Node.js and npm (CI pins Node 22 for this workspace — see
  `.github/workflows/frontend.yml`).
- Optionally, a Go toolchain matching `go/go.mod` if you want to retrieve the
  bootstrap credential with the CLI instead of reading it from the startup
  logs (see [Step 3](#step-3-retrieve-the-bootstrap-admin-credential)).

## Step 1: Start the stack

From the repository root:

```bash
docker compose up --build
```

This starts the default stack described in
[Docker Compose](../run-locally/docker-compose.md): NornicDB, Postgres, schema
migration, workspace setup, bootstrap indexing, the HTTP API on
`http://localhost:8080`, the MCP service on `http://localhost:8081`, the
ingester, and the reducer. Leave this terminal running.

## Step 2: Start the Console

In a second terminal, from the repository root:

```bash
npm install
npm run --prefix apps/console dev
```

The Console dev server proxies `/eshu-api/` to `http://127.0.0.1:8080` by
default (`apps/console/vite.config.ts`), which matches the API port Compose
just published — no extra configuration needed. Open the URL Vite prints in
the terminal (its default is `http://localhost:5173`).

With nothing saved in browser storage yet, the Console defaults to private
mode pointed at `/eshu-api/` and immediately checks
`GET /api/v0/auth/setup-state`. On a fresh stack this reports
`needs_setup: true`, so the Console shows the first-run setup wizard instead of
a login form (`apps/console/src/pages/AuthGate.tsx`).

If you open the Console over plain HTTP from anywhere other than `localhost`,
`127.0.0.0/8`, or `::1`, the session cookie will not persist after login — see
[the insecure-origin note](#insecure-origin-cookie-note) below. `localhost` is
loopback, so the default flow in this guide is unaffected.

## Step 3: Retrieve the bootstrap admin credential

Compose's default and Neo4j-compatibility files ship a fixed, publicly-known,
all-zero `ESHU_AUTH_SECRET_ENC_KEY` placeholder so the stack boots without
extra setup. With `ESHU_AUTH_BOOTSTRAP_MODE` at its default (`generated`), the
`eshu` API service seals a freshly generated one-time
username/password/recovery-code bundle on first boot. Retrieve it one of two
ways.

### Option A: Read the startup log banner

The API process prints the credential to its own stderr exactly once, on the
boot where it generates it:

```bash
docker compose logs eshu | grep -A6 "BOOTSTRAP ADMIN CREDENTIAL"
```

You will see a block like:

```text
================ ESHU BOOTSTRAP ADMIN CREDENTIAL (one-time) ================
username:      admin
password:      <generated>
recovery code: <generated>
Retrieve this again with: eshu admin initial-credential
This banner will not be shown again. Save these values now.
==============================================================================
```

### Option B: Use the CLI

If you would rather not scroll logs, or the banner already scrolled past, run
the CLI against the same Postgres the stack uses (host port `15432` by
default, per `docker-compose.yaml`):

```bash
cd go
ESHU_POSTGRES_DSN="postgresql://eshu:change-me@localhost:15432/eshu" \
ESHU_AUTH_SECRET_ENC_KEY="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" \
go run ./cmd/eshu admin initial-credential
```

This prints the same `username` / `password` / `recovery code` triple. If your
`.env` overrides `ESHU_POSTGRES_PASSWORD`, use that value instead of
`change-me`. The credential rotates the moment it is successfully claimed in
the wizard below, so `eshu admin initial-credential` only ever returns a value
before that first claim; after that, use
`eshu admin reset-initial-credential` to generate a fresh one if you need to
redo setup on a stack that already has an admin.

Save the credential from either option now — it is shown exactly once.

## Step 4: Run the first-run setup wizard

Back in the Console tab, the wizard has three steps
(`apps/console/src/pages/SetupPage.tsx`):

1. **Claim** — enter the `username` and one-time `password` from Step 3 to
   prove you own this deployment. A wrong or expired credential returns an
   error naming both CLI recovery commands
   (`eshu admin initial-credential` / `eshu admin reset-initial-credential`).
2. **Create admin** — choose your own password for the admin account. This
   replaces the generated one-time password; the username stays fixed. A
   default tenant and workspace are auto-created and assigned to this
   account.
3. **Secure** — the wizard generates a fresh set of one-time MFA recovery
   codes and shows them once, with **Copy all** and **Download .txt**
   controls. Confirm you saved them (the checkbox gates the **Finish setup**
   button) — Eshu requires MFA recovery-code proof on every admin login, so
   losing these before your first login locks you out.

Clicking **Finish setup** calls `POST /api/v0/auth/setup/mfa`, which
permanently consumes the bootstrap credential (every setup route now returns
410) and issues a browser session (`__Host-eshu_session` /
`__Host-eshu_csrf`) so you land signed in.

## Step 5: You're in

You should now be on the Console dashboard at `/`, signed in as the admin
account you just created.

## Step 6: Connect an OIDC identity provider

From the dashboard, open **Admin** in the sidebar, then the
**Identity & Access** panel's **Providers** tab
(`apps/console/src/pages/admin/AdminProvidersPanel.tsx`).

1. Click **Add provider** to open the provider drawer.
2. Fill in the OIDC fields (`apps/console/src/pages/admin/OidcProviderFields.tsx`):
   - **Issuer** — your IdP's issuer URL (e.g. Okta).
   - **Client ID**
   - **Client secret** — write-only; it is never echoed back after save.
   - **Scopes** — comma-separated, typically `openid, profile, email`.
   - **Group claim** — typically `groups`.
   - Copy the displayed **Redirect URI** and register it as an allowed
     redirect URI in your IdP's app integration before testing.
3. Click **Run test sign-in**. This saves your fields as a draft provider
   config and calls `POST /api/v0/auth/admin/provider-configs/{id}/test-connection`,
   which validates OIDC discovery/JWKS reachability and that the stored
   secret decrypts to well-formed material. It does not perform a live
   OAuth2 authorization-code round trip — that still requires signing in
   through the redirect flow once.
4. Click **Save**. Save only enables the provider
   (`POST /api/v0/auth/admin/provider-configs/{id}/enable`) when the
   immediately-preceding test passed for the exact fields on screen; editing
   any field after a test invalidates that pass and Save leaves the provider
   as a draft instead.

The provider now appears in the Providers table with an **active** status
badge, and its sign-in button appears on the login page
(`GET /api/v0/auth/providers`) for anyone visiting this tenant.

## Alternative: seed a specific admin instead of generating one

If you would rather choose the bootstrap admin's username and password
yourself instead of claiming a generated one, set both before starting the
stack:

```bash
export ESHU_ADMIN_USERNAME="you"
export ESHU_ADMIN_PASSWORD="a-password-you-choose"
docker compose up --build
```

No data-encryption key is required for this path: the password is hashed, not
sealed, and only the one-time MFA recovery code prints to the startup banner.
The setup wizard in Step 4 still runs to enroll MFA and log you in, but Step 1
of the wizard is skipped for you — you sign in directly instead
(`ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD`-seeded deployments never see the
"claim" step because there is no separate one-time credential to claim). Set
`ESHU_AUTH_BOOTSTRAP_MODE=sso-only` or `ESHU_AUTH_BOOTSTRAP_MODE=disabled` to
skip local admin seeding entirely for a deployment that only ever signs in
through SSO.

See [Environment Variable Reference](../reference/env-registry.md#auth) for
every bootstrap-related variable, its default, and its exact accepted values.

## Insecure-origin cookie note

Eshu auto-detects a plain-HTTP loopback origin (`localhost`, `127.0.0.0/8`,
`::1`) and relaxes the session cookie's `Secure` attribute only there, so
local development without TLS still keeps a persistent session. Any other
plain-HTTP origin still gets a `Secure` cookie, which the browser refuses to
store — the session will not persist, and the Console shows a diagnostic
banner rather than a confusing silent sign-out
(`ESHU_AUTH_COOKIE_SECURE`, default `auto`; see
[Environment Variable Reference](../reference/env-registry.md#api)). If you
need to reach the Console from a non-loopback plain-HTTP address, put a
TLS-terminating tunnel or proxy in front of it, or serve it over HTTPS
directly.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| Setup wizard never appears; you see a login form instead | An admin already exists, or `ESHU_AUTH_BOOTSTRAP_MODE` is `sso-only`/`disabled` | Sign in with existing credentials, or connect an OIDC provider from an existing admin session. |
| "That credential is wrong or expired" on the Claim step | The generated credential was already claimed, or `ESHU_AUTH_SECRET_ENC_KEY` changed since it was sealed | Run `eshu admin reset-initial-credential` (add `--username` if the prior credential cannot be recovered) and retry Step 3/4. |
| "Setup is no longer available" | An identity already exists in this deployment | Sign in instead; the wizard cannot be re-entered once claimed. |
| Session does not stay signed in | Console reached over a non-loopback plain-HTTP origin | See [Insecure-origin cookie note](#insecure-origin-cookie-note) above. |
| "The provider cannot be enabled: connection test did not pass" | Save was clicked before Run test sign-in passed for the current fields | Click **Run test sign-in** again, confirm it passes, then Save without editing the fields in between. |
| Console shows "Providers unavailable from this source" | The Console is not connected to a live API (demo mode, or the API is unreachable) | Confirm `docker compose ps` shows the `eshu` service healthy and the Console's proxy target matches its published port. |
| GitHub provider shows **active**, but no GitHub button appears and `GET /api/v0/auth/github/login` 404s | An active DB provider alone does not mount the route (issue #5605) | Set `ESHU_AUTH_GITHUB_ENABLED=true` and restart the API — see [HTTP API Reference](../reference/http-api.md#dashboard-browser-sessions) and [Environment Variable Reference](../reference/env-registry.md#api). |

## Related docs

- [Docker Compose](../run-locally/docker-compose.md) — full service table and
  bootstrap admin credential background.
- [Environment Variable Reference](../reference/env-registry.md) — every
  `ESHU_ADMIN_*` and `ESHU_AUTH_*` variable with its default.
- [User Management Runbook](../operate/user-management-runbook.md) — identity
  modes, tokens, roles, and audit beyond first-run setup.
- [HTTP API Reference](../reference/http-api.md) — the full `/api/v0/auth/*`
  route table.
- [Helm chart README](https://github.com/eshu-hq/eshu/blob/main/deploy/helm/eshu/README.md)
  — mounting `ESHU_AUTH_SECRET_ENC_KEY_FILE` and
  `ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD_FILE` as Kubernetes Secrets for a
  production deployment.
