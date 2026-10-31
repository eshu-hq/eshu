# Webhook Intake Host

## Purpose

`eshu-webhook-intake` is core's dedicated authenticated versioned webhook
intake host (#8876). The extracted webhook satellite verifies provider
signatures, normalizes payloads, and submits normalized triggers here over
HTTP; this host authenticates the satellite (Bearer), validates the v1 wire
form, bounds the body, and persists durable triggers in Postgres. It serves
no provider-facing routes.

## Ownership boundary

This binary owns serving core's intake API and the trigger-table schema it
writes: it is the canonical owner of `EnsureSchema` for all five trigger
tables (`webhook_refresh_triggers`, `cicd_actions_deliveries`,
`incident_freshness_triggers`, `aws_freshness_triggers`,
`gcp_freshness_triggers`). The workflow coordinator convergently runs
`EnsureSchema` for the four freshness/Actions families it already owns; that
overlap is idempotent DDL, not a second writer.

It does not own provider verification or normalization (satellite),
collector claim/handoff (`collector-git`, coordinator), fact emission, or
graph writes. It does not own the intake wire contract itself (that lives in
`go/internal/webhook/intake/`).

## Exported surface

This is a `package main`; the surface is the process plus its testable
constructors:

- `run` — telemetry bootstrap, config, Postgres, `EnsureSchema`, serve loop
- `loadWebhookIntakeConfig` — fail-closed env parsing (Bearer required)
- `buildIntakeServer` — production `intake.Server` constructor
- `openTriggerStores` — five stores plus `EnsureSchema`
- `newIntakeApplication` — admin mux plus HTTP server plus lifecycle

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/webhook/intake/` — the authenticated versioned intake API
  (reused unchanged); its tests stay with it
- `go/internal/storage/postgres/webhook/`, `.../webhook/actions/`,
  `.../freshness/{incident,aws,gcp}/` — durable trigger stores
- `go/internal/runtime/` — Postgres open, service config, admin mux, HTTP
  server, lifecycle
- `go/internal/status/` plus `go/internal/storage/postgres/` — status reader
  for `/admin/status` and `/readyz`
- `go/internal/telemetry/` — bootstrap, providers, instruments, logger

## Configuration

- `ESHU_WEBHOOK_INTAKE_TOKEN` (required) — the Bearer the satellite
  presents. Sourced from a secret object in deployment, never logged.
- `ESHU_WEBHOOK_MAX_BODY_BYTES` (default `1048576`) — one-request body cap.
  Unset means the default; an explicitly set non-positive or non-numeric
  value fails startup.
- Standard runtime config via `runtime.LoadConfig("webhook-intake")`
  (listen address, metrics address) and `runtime.OpenPostgres` (Postgres
  DSN).

## Telemetry

- Health/readiness: `/healthz` and `/readyz` answer on the shared admin
  mux (the intake subtree's own `/healthz` is shadowed there by
  longest-prefix match, as in the listener).
- Metrics: shared `telemetry.Instruments` on the `webhook-intake` service
  name plus `/metrics` via the Prometheus handler; no intake-specific
  metric names were introduced, so no dashboard renames were needed.
- Traces: OTEL providers with the `webhook-intake` service name; Postgres
  store spans flow through the instrumented stores.
- Logs: structured intake outcomes through the intake server logger
  (route, provider family, status, machine-readable reason only — Bearer
  and trigger identities never appear).

## Gotchas / invariants

- Dedicated host, not the public/read API: it mounts only the intake routes
  plus the admin surface, never provider webhook routes.
- All five stores are always wired. A family without a store would answer
  503 (keep for redelivery), so unlike the listener there is no
  token-gated store presence here.
- `EnsureSchema` failure aborts startup: serving without durable storage
  would turn submissions into 500s the provider must redeliver.
- No dual writers: on the extracted topology this host is the only intake
  writer path; the satellite performs zero SQL.
- Non-2xx responses are the redelivery mechanism — never hand-replay a
  delivery; delivery-ID keying makes redelivery idempotent.

## Related docs

- Intake package contract: `go/internal/webhook/intake/AGENTS.md`
- Listener command (provider-facing, pre-removal):
  `go/cmd/webhook-listener/AGENTS.md`
- Runtime map: `docs/public/deployment/service-runtimes-core.md`
