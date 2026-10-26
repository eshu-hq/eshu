# Supply-chain Collector Environment

This page covers SBOM, security-alert, and CI/CD run collector configuration.

## SBOM Attestation Collector

The SBOM attestation collector is claim-only. It selects an enabled
`sbom_attestation` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. Collector
configuration defines explicit `targets` with `source_type=configured_source`
for HTTP(S) document URLs or `source_type=oci_referrer` for registry referrer
documents. Supported document formats are CycloneDX, SPDX, and in-toto
statements.

`oci_referrer` targets set `provider`, `registry`, `repository`,
`subject_digest`, and `referrer_digest`. Static-credential providers read
secrets from the `username_env`, `password_env`, and `bearer_token_env`
variables. A `provider=ecr` target needs no static credentials: the collector
mints short-lived OCI Distribution basic-auth from the AWS
`GetAuthorizationToken` exchange using the AWS default credential chain, the
same auth path the OCI registry collector uses. Optional target fields `region`
and `aws_profile` select the AWS config, and `registry_host` overrides the
registry host when it differs from `registry`. Decoded ECR tokens are used only
as request credentials and are never logged.

Collectors emit typed source facts only. Reducer-owned
`reducer_sbom_attestation_attachment` facts decide whether a document subject
is attached, mismatched, unverified, parse-only, unknown, ambiguous, or
unparseable. Signature verification status remains separate from subject
attachment status.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID` | required when more than one enabled SBOM-attestation instance exists | collector-sbom-attestation | Selects the claim-capable `sbom_attestation` instance. |
| `ESHU_SBOM_ATTESTATION_POLL_INTERVAL` | `1s` | collector-sbom-attestation | Delay between empty workflow-claim polls. |
| `ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL` | `60s` | collector-sbom-attestation | Lease TTL used when claiming and refreshing work. |
| `ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL` | `20s` | collector-sbom-attestation | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID` | host/process-derived | collector-sbom-attestation | Owner label written into workflow claim rows. |

## Security Alert Collector

The security-alert collector is claim-only: it selects an enabled `security_alert` instance from
`ESHU_COLLECTOR_INSTANCES_JSON` and emits only `security_alert.repository_alert` facts. Each target polls
GitHub Dependabot alerts in one scope (`scope`, default `repository`) with one credential shape (`auth_mode`, default `token_env`):

- `scope: "repository"` polls `GET /repos/{owner}/{repo}/dependabot/alerts` and must include
  `repository` and `allowed_repositories`.
- `scope: "org"` polls `GET /orgs/{org}/dependabot/alerts` and must include `organization` and
  `allowed_repositories`, never `repository`. A truncated org read falls back to bounded per-repository reads; see
  [the org-scope window](security-intelligence-provider-alert-parity.md#org-scope-window-and-per-repository-fan-out).
- `auth_mode: "token_env"` sends a static bearer token resolved once at process startup from `token_env`.
- `auth_mode: "github_app"` mints an installation token per claim (never persisted, logged, or emitted)
  from `github_app_id_env`, `github_app_installation_id_env`, and `github_app_private_key_env`
  (the existing `GITHUB_APP_*` secret envs). The modes never mix on one target. See the
  `collector-security-alerts` binary README for the mint behavior.

Optional `api_base_url` overrides must use HTTPS because the runtime sends the bearer token to that endpoint.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID` | required when more than one enabled security-alert instance exists | collector-security-alerts | Selects the claim-capable `security_alert` instance. |
| `ESHU_SECURITY_ALERT_POLL_INTERVAL` | `1s` | collector-security-alerts | Delay between empty workflow-claim polls. |
| `ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL` | `60s` | collector-security-alerts | Lease TTL used when claiming and refreshing work. |
| `ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL` | `20s` | collector-security-alerts | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID` | host/process-derived | collector-security-alerts | Owner label written into workflow claim rows. |

Credentials must come from private env vars named by `token_env` or the `github_app_*_env`
references; never commit token values, App private keys, repository names, alert URLs, or payloads.
`eshu-collector-security-alerts --preflight-provider-access` is a one-shot access check before fanout
using the same instance JSON, credential resolution, allowlist, and client: one bounded request per target
(minting first for `github_app`), sanitized failures only. Run it against a real installation before relying
on a new App credential.

## CI/CD Run Collector

The CI/CD run collector selects an enabled, claim-capable `ci_cd_run`
instance from `ESHU_COLLECTOR_INSTANCES_JSON`. Each target requires
`provider`, `scope_id`, `repository`, and `allowed_repositories`. GitHub
Actions targets accept one of three exclusive credential shapes:
`auth_mode: "token_env"` (the default) resolves `token_env` once at startup;
`auth_mode: "host_minted"` reads a host-managed `host_token_file` for each
claim, verifying its `token` and `scope_id` and failing closed if it is stale
or unreadable; `auth_mode: "github_app"` resolves `github_app_id_env`,
`github_app_installation_id_env`, and `github_app_private_key_env` from private
pod environment variables and mints one installation token per claim. The
App installation needs Actions read permission on the target repository.
GitLab CI retains its `token_env` requirement and ignores GitHub-only fields.
GitHub `max_jobs` and `max_artifacts` must each be 1–500; `max_runs` is 0–100 with zero taking the
runtime default. Optional `api_base_url` overrides require HTTPS because the
runtime sends the bearer token to that endpoint.

The coordinator can consume durable Actions inbox entries for configured
repository scopes. The webhook listener does not yet route `workflow_run` or
`workflow_job` events into that inbox; the listener adapter and its deployment
wiring follow separately. Continue scheduled bounded collection until intake
is enabled. An inbox entry only triggers collection: the collector fetches
authoritative run, job, and artifact metadata and emits only `ci.*` source
facts. Reducers own graph and correlation truth.

Every claim cycle collects the target's most recent runs, one fully
populated fact-set per run, up to `max_runs` (an omitted or zero `max_runs`
defaults to 10; the hard cap is 100). Each run's facts are keyed by provider
run ID, so re-collecting the same window on a later cycle is an idempotent
upsert at projection rather than requiring a persistent watermark or cursor.
When the fetched runs page is full — meaning GitHub may have additional runs
beyond the window `max_runs` bounds — the collector emits a `ci.warning` fact
with `reason: "runs_truncated"` (mirroring the existing
`partial_jobs_payload` warning for a truncated jobs page) and increments
`eshu_dp_ci_cd_run_partial_generations_total{reason="runs_truncated"}`.
Widening `max_runs` (up to the 100 cap) shrinks the window between
collection cycles that a single `runs_truncated` warning can span.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` | required when more than one enabled CI/CD run instance exists | collector-cicd-run | Selects the claim-capable `ci_cd_run` instance. |
| `ESHU_CICD_RUN_POLL_INTERVAL` | `1s` | collector-cicd-run | Delay between empty workflow-claim polls. |
| `ESHU_CICD_RUN_CLAIM_LEASE_TTL` | `60s` | collector-cicd-run | Lease TTL used when claiming and refreshing work. |
| `ESHU_CICD_RUN_HEARTBEAT_INTERVAL` | `20s` | collector-cicd-run | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_CICD_RUN_COLLECTOR_OWNER_ID` | host/process-derived | collector-cicd-run | Owner label written into workflow claim rows. |

Tokens and App private keys must come from private environment variables named
by the target references. Never put resolved values, private repository names,
run URLs, artifact names, or copied payloads in public files or metric labels.
