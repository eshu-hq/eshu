# Webhook Listener Command Guidance

Read these files first:

1. `doc.go`
2. `config.go`
3. `handler.go`
4. `incident_freshness_handler.go`
5. `aws_freshness_handler.go`
6. `gcp_freshness_handler.go`
7. `gcp_freshness_oidc.go`
8. `handler_observability_test.go`
9. `main.go`
10. `handler_test.go`
11. `incident_freshness_handler_test.go`
12. `aws_freshness_handler_test.go`
13. `gcp_freshness_handler_test.go`
14. `gcp_freshness_oidc_test.go`

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
  `kind`, `action`, and `auth_path` labels.
- The GCP freshness route (`/webhook/gcp-freshness`) is default-off: it is not
  mounted unless the shared token (`ESHU_GCP_FRESHNESS_TOKEN`) or the OIDC
  pair (`ESHU_GCP_FRESHNESS_OIDC_AUDIENCE` +
  `ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA`) is configured. Both are independent,
  fail-closed accepted auth paths (#4659); either being valid is sufficient.
  Never add a third path, an anonymous bypass, or a partially-authenticated
  fallback. Never log or emit the raw OIDC token, the request's decoded
  email claim, or the shared token value — the `auth_path` metric label is
  a bounded enum (`shared_token`/`oidc`/`none`), never a raw credential.
- `verifyGCPPushOIDC` takes an injected `gcpPushOIDCValidator` so tests never
  make a live call to Google; only `googleOIDCValidator` (the production
  implementation, wrapping `google.golang.org/api/idtoken`) talks to Google.
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
- Add GCP freshness Pub/Sub push OIDC verification behavior in
  `gcp_freshness_oidc.go` and cover it in `gcp_freshness_oidc_test.go`, using
  the injected `gcpPushOIDCValidator` seam (never make a live call to Google
  in a test).
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
