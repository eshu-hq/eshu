# GCP Change Feed Operator Setup

This page walks an operator through provisioning the Google Cloud side of the
GCP change feed — a Cloud Asset Inventory (CAI) feed, a Pub/Sub topic, and a
push subscription — and enabling it on the Eshu side. It complements
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
webhook listener exposes `/webhook/gcp-freshness` as a push endpoint for that
topic. Each accepted delivery becomes a bounded, coalesced **freshness
trigger** — a wake-up signal that tells the workflow coordinator to fan out a
targeted rescan to every configured GCP collector scope matching the
notification's `(parent_scope_kind, parent_scope_id, asset_type, location)`
tuple. See
[Incremental Freshness Model](../../reference/incremental-freshness-model.md#how-webhook-triggers-differ-from-source-truth)
for how this fits the general webhook/polling relationship in Eshu: the feed
makes refresh timely, but the claim-driven GCP collector poll remains the
authoritative backfill if a notification is missed, delayed, or filtered.

The webhook route never reads the CAI asset body. It decodes only the
delivery's `messageId`, the asset's ancestry (to derive parent scope kind and
ID), asset type, resource location, and observed time — enough to target a
rescan, nothing else.

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
API scopes beyond identity — Pub/Sub uses it only to mint the OIDC token
attached to each push request. Do not reuse a broadly-privileged service
account, and do not create or download a long-lived JSON key for it.

```bash
gcloud iam service-accounts create PLACEHOLDER-GCP-FRESHNESS-PUSH-SA \
  --project=PLACEHOLDER-PROJECT \
  --display-name="Eshu GCP freshness push"
```

Grant the Pub/Sub service agent permission to mint tokens for this service
account, and grant the service account permission to publish OIDC-authenticated
push requests:

```bash
gcloud iam service-accounts add-iam-policy-binding \
  PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com \
  --project=PLACEHOLDER-PROJECT \
  --member="serviceAccount:service-PLACEHOLDER-PROJECT-NUMBER@gcp-sa-pubsub.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountTokenCreator"
```

### 5. Create the push subscription

```bash
gcloud pubsub subscriptions create PLACEHOLDER-GCP-FRESHNESS-SUB \
  --project=PLACEHOLDER-PROJECT \
  --topic=PLACEHOLDER-GCP-FRESHNESS-TOPIC \
  --push-endpoint=https://PLACEHOLDER-ESHU-HOSTNAME/webhook/gcp-freshness \
  --push-auth-service-account=PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com \
  --push-auth-token-audience=https://PLACEHOLDER-ESHU-HOSTNAME/webhook/gcp-freshness
```

Configuring `--push-auth-service-account` and `--push-auth-token-audience` is
still the right thing to do even though Eshu does not verify the resulting
OIDC token yet (see [Current auth posture](#current-auth-posture) below).
Provisioning it now means:

- Google signs and attaches the token to every push request today, so
  turning on enforcement later (issue #4659) is a one-sided Eshu change with
  no Pub/Sub-side migration.
- The push identity is still narrowly scoped and auditable in Cloud Audit
  Logs even though Eshu does not check it yet.

Set a bounded acknowledgement deadline and retry policy so a slow or briefly
unavailable Eshu endpoint does not silently drop notifications:

```bash
gcloud pubsub subscriptions update PLACEHOLDER-GCP-FRESHNESS-SUB \
  --project=PLACEHOLDER-PROJECT \
  --ack-deadline=60 \
  --min-retry-delay=10s \
  --max-retry-delay=600s
```

Pub/Sub retries a push until it is acknowledged (2xx) or the message expires
per the topic's message retention duration; the webhook route acknowledges
every request it can parse, including the benign first-delivery welcome
message and requests it rejects for auth or shape reasons, so retries are
driven by transport failures rather than by Eshu re-processing already-seen
messages. Duplicate deliveries of the same asset change coalesce into the
same durable trigger row (see [Verification](#verification)) rather than
creating duplicate rescans.

## Eshu-side configuration

### Shared freshness token

The webhook route authenticates with a shared bearer token, not per-request
GCP credentials. Generate a random, sufficiently long token and store it as a
Kubernetes Secret; do not commit it to a values file or manifest.

```bash
kubectl create secret generic PLACEHOLDER-GCP-FRESHNESS-SECRET \
  --from-literal=token=PLACEHOLDER-RANDOM-TOKEN-VALUE \
  --namespace PLACEHOLDER-NAMESPACE
```

### Helm values

Enable the route in your Helm values (see
[Helm Collector and Webhook Values](helm-collector-and-webhook-values.md#webhook-listener)
for the full `webhookListener` contract):

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
stays entirely absent from the deployment until an operator opts in.

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

Read this section before assuming OIDC protects this endpoint today.

The webhook route's **sole enforced authentication** is the shared bearer
token: either the `X-Eshu-GCP-Freshness-Token` header or an
`Authorization: Bearer <token>` header, compared against the configured token
with a constant-time check. The route fails closed — an unconfigured or empty
token never validates, and the chart does not even mount the route unless
`gcpFreshness.enabled=true` with a `secretName` set.

Pub/Sub push OIDC verification — validating the Google-signed push token's
signature, audience, and service-account principal — is **not implemented**.
The handler carries a stub (`verifyGCPPushOIDC`) that always rejects and is
not called anywhere in the request path; it exists only so the intent is
documented and tested, not as a second accepted auth mechanism. Configuring
`--push-auth-service-account` / `--push-auth-token-audience` in
[step 5](#5-create-the-push-subscription) is still worthwhile — it costs
nothing, keeps the push identity auditable, and means no subscription changes
are needed when enforcement lands — but it provides no additional protection
against a request that already carries a valid shared token, and it provides
no protection at all against a forged request if the shared token leaks.
Treat the shared token with the same care as any bearer credential: rotate it
if you suspect exposure, and restrict `/webhook/gcp-freshness` at the
ingress/network layer to Google's published Pub/Sub push IP ranges if your
environment requires defense in depth beyond the token.

OIDC enforcement is tracked in issue #4659. Enabling it will land as a Go
change plus a paired Helm `oidc` values block in the same PR — the chart does
not expose an inert `oidc` block today because a config surface that renders
no enforcement would imply protection the endpoint does not have.

## Verification

### Confirm events arrive

Trigger a change to a resource matching your feed's asset-type filter (for
example, add a label to a tracked Compute Engine instance), then check the
webhook listener's metrics:

```promql
sum(rate(eshu_dp_gcp_freshness_events_total[5m])) by (kind, action)
```

`eshu_dp_gcp_freshness_events_total` is labeled by bounded `kind`
(`asset_change`, `asset_deleted`, or `unknown`) and `action`
(`intake_stored`, `intake_coalesced`, `intake_ignored`, `intake_rejected`,
`intake_failed`). See
[Telemetry Coverage Contract](../../observability/telemetry-coverage.md) and
[Metrics — Ingestion and Collectors](../../reference/telemetry/metrics-ingestion-collectors.md)
for the full metric contract. A rising `intake_stored`/`intake_coalesced`
count after a tracked change confirms deliveries are reaching the endpoint and
authenticating successfully.

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

**All deliveries return 401 / `intake_rejected` with an auth reason.** The
shared token is the entire auth story today — this is almost always a token
mismatch, not a Google-side signing problem. Confirm the Secret referenced by
`webhookListener.gcpFreshness.secretName`/`tokenKey` matches the token value
Pub/Sub is not asked to send (Pub/Sub does not send the shared token; your
push subscription cannot supply it). Because Eshu does not verify OIDC, there
is no Google-side "wrong service account" failure mode for this route today —
if you are seeing 401s, check the token configuration on the Eshu side first,
then confirm your ingress/load balancer is not stripping the
`Authorization` or custom header before it reaches the webhook listener pod.

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
