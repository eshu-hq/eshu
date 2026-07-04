# GCP Change Feed Operator Setup

This page walks an operator through provisioning the Google Cloud side of the
GCP change feed — a Cloud Asset Inventory (CAI) feed, a Pub/Sub topic, and a
push subscription — and enabling it on the Eshu side. Eshu's webhook listener
verifies Pub/Sub's own OIDC push token directly (issue #4659), so a native
push subscription can point straight at `/webhook/gcp-freshness`; no
intermediary forwarder is required. It complements
[Helm Collector and Webhook Values](helm-collector-and-webhook-values.md),
which documents the chart's `webhookListener.gcpFreshness` block, and
[Environment Variables — Collectors](../../reference/environment-collectors.md),
which documents the webhook listener's `ESHU_GCP_FRESHNESS_*` variables.

All identifiers below (`PLACEHOLDER-PROJECT`, `PLACEHOLDER-ORG-ID`, feed and
topic names, hostnames) are examples. Substitute your own values; do not copy
these literally into a live environment.

## What this feed does

A CAI feed watches asset changes across an org, folder, or project and
publishes a notification to a Pub/Sub topic for every matching change. Eshu's
webhook listener exposes `/webhook/gcp-freshness` to receive that
notification as a bounded, coalesced **freshness trigger** — a wake-up signal
that tells the workflow coordinator to fan out a targeted rescan to every
configured GCP collector scope matching the notification's
`(parent_scope_kind, parent_scope_id, asset_type, location)` tuple. See
[Incremental Freshness Model](../../reference/incremental-freshness-model.md#how-webhook-triggers-differ-from-source-truth)
for how this fits the general webhook/polling relationship in Eshu: the feed
makes refresh timely, but the claim-driven GCP collector poll remains the
authoritative backfill if a notification is missed, delayed, or filtered.

The webhook route never reads the CAI asset body. It decodes only the
delivery's `messageId`, the asset's ancestry (to derive parent scope kind and
ID), asset type, resource location, and observed time — enough to target a
rescan, nothing else.

### Two ways to authenticate a native Pub/Sub push subscription

`/webhook/gcp-freshness` accepts two independent, fail-closed auth paths —
either is sufficient:

1. **Pub/Sub push OIDC** (recommended for a native push subscription): a
   push subscription configured with `--push-auth-service-account` has
   Pub/Sub mint a short-lived, audience-bound Google-signed OIDC token and
   attach it as `Authorization: Bearer <token>` on every push. Eshu verifies
   that token's signature against Google's public certs, checks the `aud`
   claim against the configured audience, and checks the token's `email`
   claim against the configured allowlisted service account (with
   `email_verified=true`). See
   [Eshu-side configuration](#eshu-side-configuration) below for the
   `oidc.audience` / `oidc.allowedServiceAccountEmail` values.
2. **Shared bearer token**: a static token sent as
   `X-Eshu-GCP-Freshness-Token` or `Authorization: Bearer <token>`
   (`go/cmd/webhook-listener/gcp_freshness_handler.go`,
   `validGCPFreshnessToken`), compared with a constant-time check. This
   remains useful if you already have infrastructure that injects the shared
   token — for example a proxy or forwarder in front of Eshu, or a
   non-Pub/Sub caller replaying stored deliveries.

Both fail closed independently — a request without a valid token on either
path is rejected with `401` before the body is even read. If neither is
configured, the chart does not even mount the route.

This page provisions the OIDC path directly against a native push
subscription, since it needs no extra infrastructure. If your environment
already runs a forwarder or proxy in front of Eshu for other reasons, the
[shared freshness token](#shared-freshness-token-optional-or-in-addition-to-oidc)
path still works unchanged.

## Provisioning steps (Google Cloud side)

Run these with an operator identity that has the roles below, or delegate to
your organization's standard IaC path (Terraform, Deployment Manager) if you
manage GCP resources declaratively. The commands are illustrative `gcloud`
equivalents.

### 1. Choose feed scope and asset-type filter

Decide the narrowest scope that covers what you want Eshu to track: a single
project, a folder, or an entire organization. Narrower scope means fewer
notifications and less push traffic to filter. Also decide an asset-type
filter — CAI feeds can filter by `asset_types` (for example
`compute.googleapis.com/Instance`, `storage.googleapis.com/Bucket`) so the
feed does not fire for resource kinds Eshu's GCP collector does not scan.

### 2. Create the Pub/Sub topic

```bash
gcloud pubsub topics create PLACEHOLDER-GCP-FRESHNESS-TOPIC \
  --project=PLACEHOLDER-PROJECT
```

### 3. Create the CAI feed

Project-scoped example:

```bash
gcloud asset feeds create PLACEHOLDER-GCP-FRESHNESS-FEED \
  --project=PLACEHOLDER-PROJECT \
  --pubsub-topic=projects/PLACEHOLDER-PROJECT/topics/PLACEHOLDER-GCP-FRESHNESS-TOPIC \
  --asset-types=compute.googleapis.com/Instance,storage.googleapis.com/Bucket \
  --content-type=resource
```

Folder- or org-scoped feeds use `--folder=PLACEHOLDER-FOLDER-ID` or
`--organization=PLACEHOLDER-ORG-ID` in place of `--project`, and require the
feed-provisioning identity to hold `roles/cloudasset.owner` (or an equivalent
custom role granting `cloudasset.feeds.create`) at that scope. A
project-scoped feed only needs that role on the project.

### 4. Create a least-privilege push service account

Create a dedicated service account for the push subscription. It needs no GCP
API scopes beyond identity. In a push subscription, Pub/Sub itself mints a
short-lived, audience-bound OIDC token for this service account and attaches
it to every push request — the service account does not "publish" anything
and does not need any role beyond letting Pub/Sub impersonate it for token
minting. Do not reuse a broadly-privileged service account, and do not create
or download a long-lived JSON key for it.

```bash
gcloud iam service-accounts create PLACEHOLDER-GCP-FRESHNESS-PUSH-SA \
  --project=PLACEHOLDER-PROJECT \
  --display-name="Eshu GCP freshness push"
```

Grant the Pub/Sub service agent permission to mint OIDC tokens for this
service account (this is the only IAM binding the push service account
needs):

```bash
gcloud iam service-accounts add-iam-policy-binding \
  PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com \
  --project=PLACEHOLDER-PROJECT \
  --member="serviceAccount:service-PLACEHOLDER-PROJECT-NUMBER@gcp-sa-pubsub.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountTokenCreator"
```

### 5. Create the push subscription

Point the subscription's push endpoint directly at Eshu's webhook listener.
`--push-auth-service-account` tells Pub/Sub which service account to mint
OIDC tokens for; `--push-auth-token-audience` sets the token's `aud` claim,
which must match `oidc.audience` in
[Eshu-side configuration](#eshu-side-configuration) below.

```bash
gcloud pubsub subscriptions create PLACEHOLDER-GCP-FRESHNESS-SUB \
  --project=PLACEHOLDER-PROJECT \
  --topic=PLACEHOLDER-GCP-FRESHNESS-TOPIC \
  --push-endpoint=https://PLACEHOLDER-ESHU-HOSTNAME/webhook/gcp-freshness \
  --push-auth-service-account=PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com \
  --push-auth-token-audience=https://PLACEHOLDER-ESHU-HOSTNAME/webhook/gcp-freshness
```

Set a bounded acknowledgement deadline and retry policy so a slow or briefly
unavailable Eshu endpoint does not silently drop notifications:

```bash
gcloud pubsub subscriptions update PLACEHOLDER-GCP-FRESHNESS-SUB \
  --project=PLACEHOLDER-PROJECT \
  --ack-deadline=60 \
  --min-retry-delay=10s \
  --max-retry-delay=600s
```

Pub/Sub only treats HTTP `102`, `200`, `201`, `202`, or `204` as
acknowledgment; any other status is a delivery failure and Pub/Sub retries
with backoff until the message is acknowledged or its retention window
expires (see
[Push subscriptions](https://cloud.google.com/pubsub/docs/push)). This
matters because `/webhook/gcp-freshness` returns non-2xx for more than just
transport errors: a request that fails both accepted auth paths (shared token
and OIDC) returns `401`, and a request whose decoded payload does not match
the expected `TemporalAsset` shape returns `400`. Both are **retried by
Pub/Sub**, not silently dropped — see
[Push retry semantics](#push-retry-semantics) in Troubleshooting for what
that means operationally. Only a successful store
(`202`) and the benign first-delivery welcome message (also `202`,
acknowledged without storing a trigger) stop Pub/Sub from retrying. Duplicate
deliveries of the same asset change that do reach Eshu successfully coalesce
into the same durable trigger row (see [Verification](#verification)) rather
than creating duplicate rescans.

## Eshu-side configuration

### OIDC (recommended for a native push subscription)

No secret material is needed for OIDC — Eshu verifies Google's own signed
token rather than a static credential. You only need the audience string
(matching the subscription's `--push-auth-token-audience` from
[step 5](#5-create-the-push-subscription)) and the push service account's
email (from [step 4](#4-create-a-least-privilege-push-service-account)).

```yaml
webhookListener:
  enabled: true
  gcpFreshness:
    enabled: true
    path: /webhook/gcp-freshness
    oidc:
      enabled: true
      audience: https://PLACEHOLDER-ESHU-HOSTNAME/webhook/gcp-freshness
      allowedServiceAccountEmail: PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com
```

`audience` and `allowedServiceAccountEmail` render as plain (non-secret)
environment values — they are a reference URL and a service-account email,
never credential material. Both must be set together; the chart mounts the
route once `gcpFreshness.enabled=true` and at least one auth path
(`oidc` or the shared token below) is configured.

### Shared freshness token (optional, or in addition to OIDC)

The shared bearer token remains a fully independent, optional second auth
path — useful if you already inject a static token from other infrastructure
in front of Eshu, or if you prefer not to run OIDC verification. Generate a
random, sufficiently long token and store it as a Kubernetes Secret; do not
commit it to a values file or manifest.

```bash
kubectl create secret generic PLACEHOLDER-GCP-FRESHNESS-SECRET \
  --from-literal=token=PLACEHOLDER-RANDOM-TOKEN-VALUE \
  --namespace PLACEHOLDER-NAMESPACE
```

```yaml
webhookListener:
  enabled: true
  gcpFreshness:
    enabled: true
    path: /webhook/gcp-freshness
    secretName: PLACEHOLDER-GCP-FRESHNESS-SECRET
    tokenKey: token
```

`secretName` must reference the Secret created above; `tokenKey` must match
the key used in `--from-literal`. The chart renders this token read-only into
the webhook-listener Deployment via `secretKeyRef` and does not log it. The
Ingress path is only rendered when `gcpFreshness.enabled=true`, so the route
stays entirely absent from the deployment until an operator opts into at
least one auth path. See
[Helm Collector and Webhook Values](helm-collector-and-webhook-values.md#webhook-listener)
for the full `webhookListener` contract.

Only the GCP collector's own workflow claim path (its `instanceId` and
`collectorInstances` entry, documented in
[Helm Collector and Webhook Values](helm-collector-and-webhook-values.md#collector-values))
needs to be active for a freshness trigger to result in a real rescan; the
freshness route itself has no separate scope allowlist beyond the trigger's
own `(parent_scope_kind, parent_scope_id, asset_type, location)` tuple
matching a configured, enabled GCP collector instance scope.

### Reducing poll cadence once the feed is live

GCP collector work is normally driven by the workflow coordinator's periodic
reconcile loop
(`ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL`, default `30s`; see
[Environment Variables — Collectors](../../reference/environment-collectors.md)).
Once the CAI feed is delivering freshness triggers reliably, near-real-time
change detection no longer depends solely on that poll cadence — the feed
triggers a targeted rescan of the affected scope as soon as a notification
arrives. Operators who want to reduce baseline poll load can widen
`ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` after confirming triggers are
flowing end to end (see [Verification](#verification)). Polling remains the
authoritative backfill for missed or filtered notifications, so do not disable
it outright — widen the interval, do not set it to zero.

## Current auth posture

`/webhook/gcp-freshness` accepts two independent, fail-closed auth paths —
either being valid is sufficient, and neither weakens the other:

- **Pub/Sub push OIDC**: Eshu verifies the Google-signed push token's
  signature against Google's public certs (via
  `google.golang.org/api/idtoken`), checks the `aud` claim against the
  configured `oidc.audience`, and checks the token's `email` claim against
  `oidc.allowedServiceAccountEmail` with `email_verified=true`. A missing
  token, bad signature, wrong audience, unverified email, or disallowed
  principal all fail closed.
- **Shared bearer token**: either the `X-Eshu-GCP-Freshness-Token` header or
  an `Authorization: Bearer <token>` header, compared against the configured
  token with a constant-time check. An unconfigured or empty token never
  validates.

The chart does not mount the route at all unless `gcpFreshness.enabled=true`
with at least one of the two auth paths configured.

Treat the shared token (if configured) with the same care as any bearer
credential: rotate it if you suspect exposure. Do not rely on source-IP
allowlisting as a substitute control for either path — Pub/Sub push traffic
does not originate from a small, stable, documented IP range, so an IP
allowlist either gives a false sense of protection or breaks legitimate
delivery when Google's infrastructure changes.

If your environment already runs a forwarder or proxy in front of Eshu for
unrelated reasons, it continues to work via the shared-token path unchanged;
nothing about OIDC support requires removing it.

## Verification

### Confirm events arrive

Trigger a change to a resource matching your feed's asset-type filter (for
example, add a label to a tracked Compute Engine instance), then check the
webhook listener's metrics:

```promql
sum(rate(eshu_dp_gcp_freshness_events_total[5m])) by (kind, action, auth_path)
```

`eshu_dp_gcp_freshness_events_total` is labeled by bounded `kind`
(`asset_change`, `asset_deleted`, or `unknown`), `action`
(`intake_stored`, `intake_coalesced`, `intake_ignored`, `intake_rejected`,
`intake_failed`), and `auth_path` (`shared_token`, `oidc`, or `none` — which
accepted auth path, if any, authenticated the request; never a raw header,
token, or claim value). See
[Telemetry Coverage Contract](../../observability/telemetry-coverage.md) and
[Metrics — Ingestion and Collectors](../../reference/telemetry/metrics-ingestion-collectors.md)
for the full metric contract. A rising `intake_stored`/`intake_coalesced`
count after a tracked change confirms deliveries are reaching the endpoint and
authenticating successfully; `auth_path` confirms which mechanism is actually
being used, so you can tell an OIDC-only setup is really authenticating over
OIDC rather than an unexpectedly-still-configured shared token.

The very first delivery to a newly created subscription is a benign
Pub/Sub welcome message, not an asset notification; the route acknowledges it
and records `action="intake_ignored"` rather than treating it as malformed.
Do not use that first delivery alone as your confirmation signal — wait for a
real tracked-resource change.

### Confirm coalescing

Repeated notifications for the same `(parent_scope_kind, parent_scope_id,
asset_type, location)` tuple within the same queued window collapse into one
durable trigger row rather than creating duplicate rescans; `action="intake_coalesced"`
in the metric above reflects this. This is expected behavior when a resource
changes multiple times in quick succession, not a sign of dropped events.

### Confirm a targeted rescan

Watch the fan-out histogram on the coordinator:

```promql
histogram_quantile(0.95, sum(rate(eshu_dp_gcp_freshness_fanout_scope_count_bucket[5m])) by (le))
```

`eshu_dp_gcp_freshness_fanout_scope_count` records how many configured GCP
collector scopes one trigger resolved to. A nonzero count after a tracked
change confirms the coordinator matched the trigger to at least one collector
scope and created (or reused) workflow work for it. Cross-check against the
GCP collector's own status/telemetry surfaces
(see [GCP Cloud Collector Contract](../../reference/gcp-cloud-collector-contract.md))
to confirm the resulting scan actually ran and refreshed the target's graph
state.

## Troubleshooting

### Push retry semantics

Pub/Sub only counts HTTP `102`,
`200`, `201`, `202`, or `204` from the push endpoint as an acknowledgment
([Push subscriptions](https://cloud.google.com/pubsub/docs/push)). Every
other status is a delivery failure that Pub/Sub retries with backoff, up to
the subscription's message retention window (and to a dead-letter topic if
one is configured). `/webhook/gcp-freshness` returns `401` when a request
fails both accepted auth paths (OIDC and shared token) and `400` for a
payload that does not decode as the expected `TemporalAsset` shape — both of
those are retried, not silently dropped. Only a successful store and the
first-delivery welcome message return `202`. If Eshu is misconfigured, expect
`numUndeliveredMessages` on the subscription to climb and eventual
dead-lettering (if configured) rather than quiet message loss — that is the
signal to watch, not an absence of errors.

**All deliveries return 401 / `intake_rejected` with an auth reason.** Check
the webhook listener's structured logs for the request's auth path outcome
(never the token or claim values themselves — see
[Confirm events arrive](#confirm-events-arrive) for the bounded `auth_path`
metric label). For the OIDC path, confirm `oidc.audience` exactly matches the
subscription's `--push-auth-token-audience` and `oidc.allowedServiceAccountEmail`
exactly matches `--push-auth-service-account`; a scheme, trailing slash, or
casing mismatch on the audience is a common cause. For the shared-token path,
confirm the Secret referenced by
`webhookListener.gcpFreshness.secretName`/`tokenKey` matches the literal
token value the caller sends as `X-Eshu-GCP-Freshness-Token`. If you point a
native push subscription directly at Eshu (no intermediary), a 401 here means
neither path validated — it is not a forwarder problem, since there is no
forwarder in that topology. If you do run a proxy, ingress, or load balancer
in front of the webhook-listener pod, confirm it is not stripping the
`Authorization` or `X-Eshu-GCP-Freshness-Token` header.

**No events arrive at all.** Confirm the subscription's push endpoint
resolves and is reachable from Google's Pub/Sub infrastructure (a private or
firewalled endpoint needs a public ingress or a
[Private Service Connect](https://cloud.google.com/vpc/docs/private-service-connect)
path). Check `gcloud pubsub subscriptions describe PLACEHOLDER-GCP-FRESHNESS-SUB`
for `numUndeliveredMessages` growth. If the subscription topic has no
messages at all, verify the CAI feed's `asset-types` filter actually matches
resources changing in your scope, and that the feed-provisioning identity
still has its `cloudasset` role (a revoked role does not delete the feed but
can stop new deliveries in a way that is easy to miss until you check the
feed's own audit trail).

**Events arrive but no rescan happens.** Confirm the GCP collector has an
enabled `collectorInstances` entry whose scope matches the trigger's
`(parent_scope_kind, parent_scope_id, asset_type, location)` tuple, and that
the same instance is present in `workflowCoordinator.collectorInstances`
(see [Claim-Driven Contract](helm-collector-and-webhook-values.md#claim-driven-contract)).
A trigger with no matching collector scope is marked failed with reason
`unauthorized_target` rather than silently dropped — check coordinator logs
for that reason string.

**Stale feed fallback.** If the feed or subscription is deleted, paused, or
misconfigured, Eshu has no way to detect that from the webhook side alone —
the route simply stops receiving deliveries. The claim-driven GCP collector
poll (`ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL`) is the safety net: it
continues to re-observe configured scopes on its normal cadence regardless of
feed health, so leave that poll enabled (do not disable it based on
[Reducing poll cadence](#reducing-poll-cadence-once-the-feed-is-live) above)
even after the feed is verified working.

## Related docs

- [Helm Collector and Webhook Values](helm-collector-and-webhook-values.md)
- [Environment Variables — Collectors](../../reference/environment-collectors.md)
- [Incremental Freshness Model](../../reference/incremental-freshness-model.md)
- [Telemetry Coverage Contract](../../observability/telemetry-coverage.md)
- [GCP Cloud Collector Contract](../../reference/gcp-cloud-collector-contract.md)
