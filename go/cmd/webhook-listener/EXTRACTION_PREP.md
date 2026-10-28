# Webhook Listener — Extraction Prep Census (`#8876`)

Status: **PREP ONLY**. No code moves, no repo creation, no deploy, no in-tree
removal. This manifest records the current in-tree truth so the cutover gate
(`#4047`) can compare in-tree and external outputs on identical inputs.

Scope: `go/cmd/webhook-listener/` (binary `eshu-webhook-listener`) and
webhook-owned implementation: `go/internal/webhook/`,
`go/internal/collector/awscloud/freshness/`,
`go/internal/collector/gcpcloud/freshness/`,
`go/internal/storage/postgres/webhook/` (trigger store + `actions/` inbox),
`go/internal/storage/postgres/freshness/{aws,gcp,incident}/`.
Out of scope: peer-owned intake-boundary + compat proof (`@linuxdynasty`
claim on `#8876`, 2026-10-24 — reused here, not redone), canonical fact
admission and recovery (core-owned), all other collectors, org repos,
deployment.

## 1. Current packages

### Binary: `go/cmd/webhook-listener/` (478-line `handler.go`, 103-line `config.go`, 220-line `main.go`)

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go` | entry point, Postgres wiring, per-store `EnsureSchema` (`run`) | `runtimecfg.OpenPostgres`, `NewWebhookTriggerStore`, `actionsstore.NewStore`, `NewIncidentFreshnessStore`, `NewAWSFreshnessStore`, `NewGCPFreshnessStore` |
| `config.go` | 7 provider secrets/paths, 1 MiB default body cap | `defaultMaxWebhookBodyBytes`, `MaxRequestBodyBytes` (`loadWebhookListenerConfig`) |
| `handler.go` | 7 mounted provider routes via `newWebhookMux` | `handleGitHub`, `handleGitLab`, `handleBitbucket`, `handlePagerDuty`, `handleJira`, `handleAWSFreshnessEventBridge`, `handleGCPFreshnessPubSubPush` |
| `handler.go` | bounded body read: 413 only on cap exceed, else 400 | `readPostBody` over `http.MaxBytesReader` |
| `actions_handler.go` | Actions inbox: exact-duplicate 202, conflicting-identity 409, missing store 503 | delivery-identity check |
| `incident_freshness_handler.go` | PagerDuty/Jira scoped incident wake-ups | incident freshness route |
| `aws_freshness_handler.go` | EventBridge/Config `(account_id, region, service_kind)` wake-ups | AWS freshness route |
| `gcp_freshness_handler.go`, `gcp_freshness_oidc.go` | CAI Pub/Sub push wake-ups, shared-token OR OIDC (default-off, fail-closed) | `verifyGCPPushOIDC` (injected validator seam) |

### Library: `go/internal/webhook/` (moves with the listener)

| File | Role | Key symbol |
| --- | --- | --- |
| `signature.go` | provider verification | `VerifyGitHubSignature`, `VerifyBitbucketSignature`, `VerifyJiraSignature`, `VerifyPagerDutySignature`, `VerifyGitLabToken` |
| `normalizer_github.go`, `normalizer_gitlab.go`, `normalizer_bitbucket.go` | git-provider normalization, accept/ignore decisions | provider normalizers |
| `normalizer_ado.go`, `normalizer_jenkins.go`, `normalizer_circleci.go`, `normalizer_buildkite.go`, `normalizer_drone.go` | CI-provider normalization | CI normalizers |
| `actions.go` | Actions identity/delivery-conflict rules | Actions inbox rules |
| `incident_freshness.go` | PagerDuty/Jira incident wake-up normalization | incident normalizer |

### Freshness normalizers (move with the listener)

- `go/internal/collector/awscloud/freshness/eventbridge.go`, `types.go`
- `go/internal/collector/gcpcloud/freshness/pubsub.go`, `types.go`

### Durable stores (core Postgres owns durable state; store packages move with the listener)

- `go/internal/storage/postgres/webhook/trigger_store_sql.go` —
  `ON CONFLICT (refresh_key) DO UPDATE` (`storeWebhookTriggerQuery`),
  `duplicate_count+1`, `ORDER BY received_at,trigger_id FOR UPDATE SKIP
  LOCKED` (`claimQueuedWebhookTriggersQuery`), `claim_fencing_token+1`,
  token-checked completion (`markWebhookTriggersHandedOffQueryFormat`),
  stale-claim reap (`reapStaleWebhookTriggerClaimsQuery`).
- `go/internal/storage/postgres/webhook/actions/` — `cicd_actions_deliveries`
  inbox (`store.go`, `sql.go`, `schema.go`).
- `go/internal/storage/postgres/freshness/{aws,gcp,incident}/` — one
  `store.go` + `sql.go` + `schema_sql.go` each. Incident store claims via
  `FOR UPDATE SKIP LOCKED` (`claimQueuedIncidentFreshnessTriggersQuery`)
  with `ClaimQueuedTriggers`, `MarkTriggersHandedOff`,
  `MarkTriggersFailed` (all in incident `store.go`) — and **no**
  fencing-token/lease column: lease/fencing recovery
  is the explicit unproven prerequisite (see §9.1).

## 2. Fact kinds and versions

**None.** The listener emits no facts (`cmd` `README.md`/`doc.go`: payloads
never become CI/CD, incident, or graph facts; payloads are wake-up triggers
only). SDK relocation (`#6708`) is therefore NOT a prerequisite for this
lane — zero `sdk/go/collector` or `factschema` imports in the scoped
sources. Deployment-compat preparation may proceed independently per the
`#8876` body.

## 3. Consumers (must agree at cutover)

- Git collector: `go/cmd/collector-git/service.go`,
  `go/internal/collector/repo/git/webhook_trigger_selector.go`
  (reads `webhook_refresh_triggers`).
- CI/CD coordinator Actions inbox: `go/internal/coordinator/service_cicd_run_webhook.go`.
- Freshness coordinator planners: `go/internal/coordinator/service_aws_freshness.go`,
  `service_gcp_freshness.go`, `service_incident_freshness.go`.
- Communication boundary: provider HTTP intake → Postgres trigger tables →
  collector handoff. No graph connection (`doc.go`).

## 4. Durable-state owner

Core Postgres owns durable trigger storage and lifecycle. The listener
touches only `webhook_refresh_triggers`, `cicd_actions_deliveries`,
`aws_freshness_triggers`, `gcp_freshness_triggers`,
`incident_freshness_triggers` through its stores (`CREATE TABLE IF NOT
EXISTS` in each `schema_sql.go`); no fact, graph, reducer, or core-table
mutation. Admission, persistence, reducer queue, projection, and retraction
remain Eshu core. The extracted runtime must verify, normalize, and submit
through the supported intake contract (`@linuxdynasty` claim); it never
receives Postgres, graph, queue, lease, reducer, API, MCP, or workflow
handles.

## 5. Protocols

Seven mounted provider routes (`newWebhookMux` in `handler.go`), each gated on its
configured secret/token and unmounted when unconfigured:

| Provider | Auth | Delivery identity |
| --- | --- | --- |
| GitHub `X-Hub-Signature-256` | HMAC-SHA256 `sha256=` (`VerifyGitHubSignature`); SHA-1 rejected | `X-GitHub-Delivery` required before normalization |
| GitLab `X-Gitlab-Token` | constant-time compare (`VerifyGitLabToken`) | `Idempotency-Key` preferred, then provider UUID |
| Bitbucket `X-Hub-Signature` | HMAC (`VerifyBitbucketSignature`) | `X-Request-UUID` preferred, then `X-Hook-UUID` |
| AWS freshness | `Authorization: Bearer` or `X-Eshu-AWS-Freshness-Token` | normalized `(account_id, region, service_kind)` tuple |
| GCP freshness (default-off) | shared token OR Pub/Sub push OIDC (`aud` + allowlisted SA `email`/`email_verified`), either sufficient, fail-closed | normalized `(parent_scope_kind, parent_scope_id, asset_type, location)`; raw CAI `resource.data` dropped at the normalizer redaction choke point; bare-string welcome message acked 202 without storing |
| PagerDuty `X-PagerDuty-Signature` | `v1=` multi-candidate (`VerifyPagerDutySignature`) | payload `event.id`, `X-Webhook-Id`, or `X-Request-Id`; scoped by `ESHU_WEBHOOK_PAGERDUTY_SCOPE_ID` |
| Jira `X-Hub-Signature` | HMAC (`VerifyJiraSignature`) | `X-Atlassian-Webhook-Identifier` or `X-Request-Id`; only issue created/updated/deleted admitted, other families rejected `reason=unsupported_event`; scoped by `ESHU_WEBHOOK_JIRA_SCOPE_ID` |

Body bound: `ESHU_WEBHOOK_MAX_BODY_BYTES`, default 1 MiB (`defaultMaxWebhookBodyBytes`);
413 only on cap exceed, other read failures 400 (`readPostBody`).
GitHub `ping` is a verified no-op handshake (202, no trigger).
Duplicates: same delivery ID dedupes/coalesces (`duplicate_count+1`);
conflicting identity returns 409 with the original row intact. Store
failure returns 500/503 so providers redeliver (durable retry without
direct core-table mutation).

Telemetry (bounded labels only — provider, event kind, decision, status,
outcome, reason, freshness kind/action, `auth_path`; delivery IDs, repos,
SHAs, ARNs, OIDC claims stay out): `eshu_dp_webhook_requests_total`,
`eshu_dp_webhook_trigger_decisions_total`,
`eshu_dp_webhook_store_operations_total`,
`eshu_dp_aws_freshness_events_total`, `eshu_dp_gcp_freshness_events_total`,
`eshu_dp_webhook_request_duration_seconds`,
`eshu_dp_webhook_store_duration_seconds`, spans `webhook.handle` /
`webhook.store`; `/healthz`, `/readyz`, `/metrics`, `/admin/status` on the
shared runtime mux.

## 6. Artifacts

- Today: single in-tree binary `eshu-webhook-listener` (`main.go`);
  module `github.com/eshu-hq/eshu/go`, go 1.26.10. No published artifact.
- Chart: `deploy/helm/eshu/templates/deployment-webhook-listener.yaml`
  (+ service/ingress/networkpolicy/pdb) on the single shared chart image
  `.Values.image.repository:tag` — no per-webhook artifact version.
- At cutover: destination `eshu-hq/eshu-webhook` holds only a placeholder
  `README.md` (default branch `main`, HEAD `9580c04b` observed 2026-10-28;
  the `#8876` body cites inventory main
  `50e556470fcddc99c953c7662ed4b0bc1995674e` — re-inventory at cutover).
  No code moved; no cutover authority.

## 7. Migration stage

`contract_blocked` (amended 2026-10-28; see §10). This PREP
records evidence and grants no cutover authority. Still not
`ready_for_cutover`: peer intake-boundary/compat proof across seven
providers and four trigger stores is peer-owned work in progress (reused,
not redone); incident freshness lease/fencing recovery is the explicit
unproven prerequisite; extracted-path parity, independent
build/release/deploy, rollback rehearsal, and production acceptance are
`NOT_CHECKED`.

## 8. `#8876` acceptance sweep at `origin/main`

| Acceptance item | State |
| --- | --- |
| Inventory verification/signature rules, replay/dedup keys, trigger state, durable handoff ownership | **Met in PREP** by §§1–5 |
| Valid/invalid signatures, duplicates, ordering, bounded bodies, durable retry/partial-failure without core-table mutation | **Partial (in-tree side)** — `handler_test.go` pins accept/reject per provider (accept: `TestWebhookHandlerAcceptsSignedGitHubPush`, `TestWebhookHandlerAcceptsSignedGitHubPingWithoutStoringTrigger`, `TestWebhookHandlerAcceptsGitLabToken`, `TestWebhookHandlerAcceptsSignedBitbucketPush`, `TestWebhookHandlerAcceptsSignedPagerDutyFreshnessTrigger`, `TestWebhookHandlerAcceptsSignedJiraFreshnessTrigger`; reject: `TestWebhookHandlerRejectsMissingPagerDutySignature`, `TestWebhookHandlerRejectsMissingPagerDutyDeliveryIdentity`, `TestWebhookHandlerRejectsMissingJiraDeliveryID`, `TestWebhookHandlerRejectsBadGitHubSignature`, `TestWebhookHandlerRejectsMissingGitHubDeliveryID`, `TestWebhookHandlerRejectsMissingBitbucketDeliveryID`, `TestWebhookHandlerRejectsMissingGitLabDeliveryID`), body 413-vs-400 (`TestWebhookHandlerDistinguishesBodyReadErrors`, `TestWebhookHandlerReportsOversizedBody`), Actions 202/409/503; dedupe/coalescing + fencing-claim store SQL (§1); explicit ordering test NOT found; extracted-path proof pending peer compat work |
| Named owners, production target, configuration source | **Partial** — owner-confirmed on the issue (`@linuxdynasty` release/deployment owner, QA EKS target); configuration source still to be established from the caller inventory |
| Census, kinds, consumers, state, protocols, artifacts, stage | **Met in PREP** by this document |
| Behavior + public-contract parity on representative/hostile inputs | **Open** — in-tree tests listed above; extracted-path proof outstanding |
| Extracted deployed path preserves reducer/graph/API/MCP truth | **NOT_CHECKED** (read-only intake move; surfaces unaffected except via collector handoff — to be proven) |
| Concurrent retry, duplicates, ordering, cancellation, revoked authority, stale claims, partial/ambiguous failures | **Open** — claim-lease integration suites exist but gate on live Postgres DSNs (`ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN` et al.); incident lease/fencing explicitly unproven |
| Bounded resources, no regression, telemetry | **Partial** — bounds + signals recorded in §5 and `cmd` README; extracted-path proof outstanding |
| Credentials, provenance, vuln response, oldest/newest pairs | **Open** — credential env surface recorded in §5; pairs need template/harness (`#6710`) |
| Independent build/release/deploy on public contracts | **Open** — dest holds only the placeholder README |
| Rollback rehearsal | **Open** — no draft recorded for this lane |
| Review, local proof, stable CI, removal after acceptance | **Open** — local proof in this lane's commit; removal explicitly **not** in PREP |

## 9. Prerequisites for cutover

1. Peer intake-boundary + compat proof across seven mounted providers and
   four trigger stores (`@linuxdynasty` claim on `#8876`, 2026-10-24):
   direct-store vs authenticated versioned HTTP intake with core retaining
   durable trigger storage/lifecycle. Reused, not redone, by this PREP.
2. Incident freshness lease/fencing recovery proof — explicit unproven
   prerequisite; incident store has claim/handoff/fail transitions but no
   lease/fencing token (§1).
3. Extracted-path parity (valid/invalid signatures, duplicates, ordering,
   bounded bodies, durable retry/partial-failure) through the supported
   intake contracts.
4. Destination repository from the standalone template with the
   compatibility harness matrix (`#6710`; newest webhook × oldest core and
   reverse), independent build/release/deploy, credential/provenance
   record, and oldest/newest version pairs.
5. Rehearsed rollback for this state boundary (restore prior compatible
   artifacts/configuration; fence claims, revoke replaced authority,
   replay without dual authority or lost facts).
6. Dest re-inventory at cutover (`50e55647` cited vs `9580c04b` observed).

## 10. Stage amendment — arbiter-eshu revised ruling (2026-10-28)

- Stage is now `contract_blocked` on `#9377` (authenticated versioned
  HTTP intake API replacing direct trigger-table writes) and `#9379`
  (incident lease/fencing recovery): no cutover work proceeds until both
  gates clear.
- Unaffected surfaces: reducer, graph, API, and MCP truth are out of scope
  for this lane — the listener emits zero fact kinds (§2), so extraction
  cannot change query truth except via the collector handoff boundary.
- Boundary distinction: a deployment-only move (packaging the intake
  runtime) stays separable from the full-boundary move (cutting the
  trigger-table write path over to the intake contract); only the latter
  needs the `#9377`/`#9379` gates.
- Stage comments already posted on `#8876`; not reposted.
