# CI/CD Run Telemetry

## CI/CD Run Collector

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_ci_cd_run_provider_requests_total` | `provider`, `status_class` | Hosted provider attempts. Direct App authentication/configuration failures and non-rate-limited provider-read 401/403/404 use `auth_error`; throttled provider reads or mints use `rate_limited`; host-token file or mandatory authentication refusals use `auth_denied`. Static-token errors retain `error`. Credential values are never labels. |
| `eshu_dp_ci_cd_run_facts_emitted_total` | `provider`, `fact_kind` | `ci.*` source facts emitted per claimed target. |
| `eshu_dp_ci_cd_run_rate_limited_total` | `provider` | GitHub Actions rate-limit pressure surfaced to workflow retry handling. |
| `eshu_dp_ci_cd_run_partial_generations_total` | `provider`, `reason` | Bounded partial evidence such as truncated job pages or provider warnings, including `reason="runs_backfill_gap"`: a claim cycle detected that runs between a prior cycle's watermark and this cycle's fetched window were never fetched by either cycle (#5429). |
| `eshu_dp_ci_cd_run_fetch_duration_seconds` | `provider`, `status_class` | Bounded provider fetch duration for one claimed target. |
| `eshu_dp_ci_cd_run_webhook_handoffs_total` | `outcome` | Durable Actions wake-up attempts: `admitted`, `recovered`, `blocked`, `unauthorized`, or `failed`. A sustained `blocked` increase means open work or no active eligible collector instance is preventing admission. |
| `eshu_dp_ci_cd_run_webhook_reaps_total` | `outcome` | Expired Actions delivery leases returned to the queue (`requeued`) or terminally failed after exhausting claims (`exhausted`). Counts reported transitions from successful reap calls; a warning carries aggregate counts when any exhaust. |
| `eshu_dp_ci_cd_run_webhook_lag_seconds` | none | Receipt-to-admission delay for successful or exactly recovered webhook handoffs. Watch the upper buckets for stuck wake-ups. |
| `eshu_dp_ci_cd_run_poll_fallback_total` | none | Scheduled GitHub Actions target work actually admitted as bounded backfill. Compare its rate with webhook handoffs to see fallback use. |

The Actions inbox consumer emits these handoff and reaper signals. Verified
workflow-event intake uses the shared webhook request counter and duration
metrics and the `webhook.handle` span. Its bounded event kind appears in spans
and request logs; request metric labels remain provider, outcome, and reason.
Scheduled bounded reads provide backfill when no delivery work is pending.
Intake records acceptance after the durable inbox store succeeds. Invalid
signatures use the shared rejected/auth outcome; unavailable or failing inbox
storage uses failed/store. Follow the request span to distinguish intake
failure from later admission lag.

Repository names, workflow run IDs, artifact names, URLs, token environment
names, token values, and provider response bodies stay out of metric labels.
Use `/admin/status`, workflow failures, and traces to connect a bounded failure
class to a specific private target in the operator environment.

A rise in
`eshu_dp_ci_cd_run_provider_requests_total{provider="github_actions",status_class="auth_error"}`
signals a direct-App authentication, authorization, configuration, or
resource-access failure. It does not distinguish mint failures from provider
reads. Host-token file or mandatory Actions refusals use `auth_denied`, while
static-token errors retain `error`. Optional Deployments 403/404 responses
produce partial-coverage warnings rather than terminal claim failures. Use the
`ci_cd_run.observe` span and workflow claim state to identify the stage and
cause. No credential, repository, or installation ID is a metric label.

The handoff counter records one outcome per delivery attempt, including each
delivery in a persisted repository claim group. It does not count unique
collector work items. Reap transitions use their own counter and are not
handoff attempts. `blocked` covers open work and the absence of an active
eligible instance; inspect the inbox `failure_class` to distinguish `blocked`
from `no_active_instance`. The group's deliveries remain pending. `recovered` means the
group's exact durable work was found after an earlier handoff. Lag samples
exist only for `admitted` and `recovered`; no sample is emitted for blocked or
failed deliveries. A rising upper lag bucket beside
`blocked` or `failed` outcomes calls for queue and claim inspection.

`eshu_dp_ci_cd_run_poll_fallback_total` counts scheduled GitHub Actions work actually admitted,
not every timer tick; GitLab admissions do not increment it. A rising fallback rate with falling webhook handoffs
indicates that scheduled bounded reads are carrying freshness. The fetch
window and `runs_backfill_gap` warning still limit what historic runs can be
recovered after a delivery outage.
