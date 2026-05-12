# Webhook Listener Command Guidance

Read these files first:

1. `doc.go`
2. `config.go`
3. `handler.go`
4. `handler_observability_test.go`
5. `main.go`
6. `handler_test.go`

## Invariants

- Do not add graph credentials or repository workspace mounts to this runtime.
- Do not parse provider payloads into graph facts here. Persist trigger
  decisions only.
- Keep request body limits and provider authentication before normalization.
- Keep repository names, delivery IDs, branch names, and SHAs out of metric
  labels. Use logs or spans for that detail.
- Keep public ingress limited to provider webhook routes.

## Common Changes

- Add provider configuration in `config.go`.
- Add provider route behavior in `handler.go` and cover it in
  `handler_test.go`.
- Add or change OTEL behavior in `handler.go` and cover it in
  `handler_observability_test.go`.
- Add runtime startup wiring in `main.go` only after the handler/store contract
  is tested.

## Do Not

- Do not mount `/admin/status` publicly by chart default.
- Do not store raw webhook payload bodies unless an ADR adds bounded retention
  and redaction requirements.
- Do not turn ignored trigger decisions into repository refresh work.
