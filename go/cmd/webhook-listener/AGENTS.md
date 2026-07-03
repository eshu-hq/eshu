# Webhook Listener Command Guidance

Read these files first:

1. `doc.go`
2. `config.go`
3. `handler.go`
4. `incident_freshness_handler.go`
5. `aws_freshness_handler.go`
6. `gcp_freshness_handler.go`
7. `handler_observability_test.go`
8. `main.go`
9. `handler_test.go`
10. `incident_freshness_handler_test.go`
11. `aws_freshness_handler_test.go`
12. `gcp_freshness_handler_test.go`

## Invariants

- Do not add graph credentials or repository workspace mounts to this runtime.
- Do not parse provider payloads into graph facts here. Persist trigger
  decisions only.
- Keep request body limits and provider authentication before normalization.
- Keep repository names, delivery IDs, branch names, and SHAs out of metric
  labels. Use logs or spans for that detail.
- Keep AWS resource names, IDs, ARNs, tags, and raw payloads out of metric
  labels. AWS freshness metrics use only bounded `kind` and `action` labels.
- Keep GCP resource names, parent scope ids, asset bodies, and raw Pub/Sub
  push payloads out of metric labels. GCP freshness metrics use only bounded
  `kind` and `action` labels.
- The GCP freshness route (`/webhook/gcp-freshness`) is default-off: it is not
  mounted unless `ESHU_GCP_FRESHNESS_TOKEN` is configured, and the shared
  token is the sole required auth mechanism today. Do not add an OIDC or
  anonymous fallback that would let a request through without a valid
  matching token; real Pub/Sub push OIDC verification is a separate,
  dedicated security-review change (#4339).
- Keep public ingress limited to provider webhook routes.

## Common Changes

- Add provider configuration in `config.go`.
- Add provider route behavior in `handler.go` and cover it in
  `handler_test.go`.
- Add PagerDuty or Jira incident freshness route behavior in
  `incident_freshness_handler.go` and cover it in
  `incident_freshness_handler_test.go`.
- Add AWS freshness route behavior in `aws_freshness_handler.go` and cover it
  in `aws_freshness_handler_test.go`.
- Add GCP freshness route behavior in `gcp_freshness_handler.go` and cover it
  in `gcp_freshness_handler_test.go`.
- Add or change OTEL behavior in `handler.go` and cover it in
  `handler_observability_test.go`.
- Add runtime startup wiring in `main.go` only after the handler/store contract
  is tested.

## Do Not

- Do not mount `/admin/status` publicly by chart default.
- Do not store raw webhook payload bodies unless an ADR adds bounded retention
  and redaction requirements.
- Do not turn ignored trigger decisions into repository refresh work.
- Do not decode or retain the CAI `resource.data` blob from a GCP freshness
  push delivery; the normalizer package is the single redaction choke point.
