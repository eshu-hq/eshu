# Webhook Intake Host Guidance

Read these files first:

1. `doc.go`
2. `config.go`
3. `main.go`
4. `README.md`
5. `go/internal/webhook/intake/AGENTS.md`

## Invariants

- This host serves intake only. Do not add provider webhook routes,
  provider signature verification, or provider secrets to this binary —
  those live in the satellite.
- Do not add fact emission, graph writes, queue handoff, or repository
  workspace mounts to this runtime.
- Keep `go/internal/webhook/intake/` unchanged when wiring changes here.
  Wire, validation, and receipt shapes live in that package; this binary
  only constructs and serves them.
- Keep the Bearer out of logs, errors, and metric labels. Env-var names
  may appear in errors; values never do.
- Keep all five trigger stores wired unconditionally. Do not gate store
  presence on provider tokens — this host has none.
- Keep `EnsureSchema` ownership here for all five trigger tables. The
  coordinator's overlapping `EnsureSchema` for four families is
  convergent DDL; do not remove it from here without an arbiter ruling.
- Do not widen the body cap default without a measured intake-latency
  note (same rule as the intake package).

## Common Changes

- Add host env configuration in `config.go` with fail-closed validation
  and cover it in `config_test.go` (missing, blank, default, override,
  invalid, and no-leak cases).
- Change serve wiring in `main.go` only after the store/server contract
  is tested through the production constructors (`buildIntakeServer`,
  `openTriggerStores`).
- Add or change OTEL behavior through the shared `telemetry` bootstrap
  and cover serving outcomes through the intake routes (no new metric
  names without a telemetry-coverage row and dashboard disposition).

## Do Not

- Do not mount `/admin/status` publicly by chart default.
- Do not store raw webhook payload bodies unless an ADR adds bounded
  retention and redaction requirements.
- Do not accept provider-signed payloads on these routes. Provider trust
  ends at the satellite.
- Do not return store internals in error bodies; the intake package
  already maps them to machine-readable codes.
