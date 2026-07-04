# GCP Change Feed Operator Setup

This page walks an operator through provisioning the Google Cloud side of the
GCP change feed — a Cloud Asset Inventory (CAI) feed, a Pub/Sub topic, a push
subscription, and the small push-forwarder that bridges Pub/Sub's OIDC push
auth to Eshu's shared-token auth — and enabling it on the Eshu side. It
complements
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

### Read this before provisioning: a native Pub/Sub push subscription cannot reach this endpoint today

`/webhook/gcp-freshness` authenticates a request with exactly one mechanism:
a shared bearer token, sent as `X-Eshu-GCP-Freshness-Token` or
`Authorization: Bearer <token>`
(`go/cmd/webhook-listener/gcp_freshness_handler.go`, `validGCPFreshnessToken`).
It fails closed — a request without that exact token is rejected with `401`
before the body is even read.

A **native** Google Cloud Pub/Sub push subscription cannot supply that token.
Pub/Sub authenticated push always sends its own Google-signed OIDC JWT in the
`Authorization` header, and push subscriptions have no supported mechanism to
also attach a static custom header such as
`X-Eshu-GCP-Freshness-Token` (see
[Authenticate push subscriptions](https://cloud.google.com/pubsub/docs/authenticate-push-subscriptions)
and the
[`gcloud pubsub subscriptions create` reference](https://cloud.google.com/sdk/gcloud/reference/pubsub/subscriptions/create),
whose push-auth flags cover only the endpoint and the OIDC service
account/audience). Eshu's handler does not verify that OIDC JWT — the
`verifyGCPPushOIDC` function exists in the handler but always returns
`false` and is never called on the request path; it is a documented,
tested placeholder for future work tracked in issue #4659, not a second
accepted auth path.

The net effect: **a bare native push subscription pointed directly at
`/webhook/gcp-freshness`, with no forwarder in between, will authenticate
every single delivery as a `401` and never store a freshness trigger.** Do
not set that up expecting it to work.

This page therefore documents the path that actually delivers today: a small
**push-forwarder** service that receives the OIDC-authenticated Pub/Sub push,
verifies it belongs to your feed, and re-calls `/webhook/gcp-freshness` with
the shared token attached. If your environment cannot run a forwarder, treat
the direct-push path as **blocked** until OIDC verification lands in #4659,
and fall back to relying on the claim-driven GCP collector's own poll cadence
for freshness instead.

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

### 5. Deploy a push-forwarder

Because the native push subscription's OIDC-authenticated request cannot
carry Eshu's shared token (see
[the note above](#read-this-before-provisioning-a-native-pubsub-push-subscription-cannot-reach-this-endpoint-today)),
point the push subscription at a small forwarder instead of directly at
Eshu. A minimal forwarder (Cloud Run or Cloud Functions is a natural fit,
since both integrate with Pub/Sub push and Google-managed OIDC verification
out of the box) does exactly two things per request:

1. Verifies the inbound request's OIDC token — audience matches the
   forwarder's own URL, issuer is Google, and the token's service-account
   principal matches `PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com`.
   Cloud Run and Cloud Functions can enforce this automatically when deployed
   with `--no-allow-unauthenticated` and the push subscription's
   `--push-auth-service-account` set to the same service account, so the
   platform itself rejects tokens with the wrong principal before your code
   runs.
2. Re-issues the same request body to
   `https://PLACEHOLDER-ESHU-HOSTNAME/webhook/gcp-freshness`, attaching
   `X-Eshu-GCP-Freshness-Token: PLACEHOLDER-RANDOM-TOKEN-VALUE` (the same
   value configured in [Eshu-side configuration](#eshu-side-configuration)
   below), and returns Eshu's response status back to Pub/Sub unchanged, so
   Pub/Sub's retry behavior still reflects Eshu's actual accept/reject
   decision.

Keep the forwarder to that shape. It is a token-injection relay, not a place
to add business logic, retries, or buffering — Eshu's own coalescing and
durable trigger storage already handle duplicates and retries once a request
reaches `/webhook/gcp-freshness`.

If your environment cannot run a forwarder, do not point a native push
subscription directly at `/webhook/gcp-freshness`; it will not authenticate.
Treat direct push as blocked pending #4659 and rely on the GCP collector's
own poll cadence for freshness until either the forwarder or OIDC enforcement
is in place.

### 6. Create the push subscription

Point the subscription's push endpoint at the forwarder from step 5, not
directly at Eshu:

```bash
gcloud pubsub subscriptions create PLACEHOLDER-GCP-FRESHNESS-SUB \
  --project=PLACEHOLDER-PROJECT \
  --topic=PLACEHOLDER-GCP-FRESHNESS-TOPIC \
  --push-endpoint=https://PLACEHOLDER-FORWARDER-HOSTNAME/ \
  --push-auth-service-account=PLACEHOLDER-GCP-FRESHNESS-PUSH-SA@PLACEHOLDER-PROJECT.iam.gserviceaccount.com \
  --push-auth-token-audience=https://PLACEHOLDER-FORWARDER-HOSTNAME/
```

Set a bounded acknowledgement deadline and retry policy so a slow or briefly
unavailable forwarder or Eshu endpoint does not silently drop notifications:

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
transport errors: a request with a missing or mismatched shared token
returns `401`, and a request whose decoded payload does not match the
expected `TemporalAsset` shape returns `400`. Both are **retried by Pub/Sub**,
not silently dropped — see
[Push retry semantics](#push-retry-semantics) in Troubleshooting for what
that means operationally. Only a successful store
(`202`) and the benign first-delivery welcome message (also `202`,
acknowledged without storing a trigger) stop Pub/Sub from retrying. Duplicate
deliveries of the same asset change that do reach Eshu successfully coalesce
into the same durable trigger row (see [Verification](#verification)) rather
than creating duplicate rescans.

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

Read this section before assuming OIDC protects `/webhook/gcp-freshness`
directly.

The webhook route's **sole enforced authentication** is the shared bearer
token: either the `X-Eshu-GCP-Freshness-Token` header or an
`Authorization: Bearer <token>` header, compared against the configured token
with a constant-time check. The route fails closed — an unconfigured or empty
token never validates, and the chart does not even mount the route unless
`gcpFreshness.enabled=true` with a `secretName` set.

Pub/Sub push OIDC verification at the Eshu handler — validating the
Google-signed push token's signature, audience, and service-account
principal — is **not implemented**. The handler carries a stub
(`verifyGCPPushOIDC`) that always rejects and is not called anywhere in the
request path; it exists only so the intent is documented and tested, not as
a second accepted auth mechanism against `/webhook/gcp-freshness` itself.
This is exactly why this page routes push traffic through the forwarder in
[step 5](#5-deploy-a-push-forwarder): the forwarder is where OIDC
verification actually happens today (enforced by the platform when deployed
with `--no-allow-unauthenticated`), and the forwarder is the piece that
translates a verified request into Eshu's shared-token contract.

Treat the shared token with the same care as any bearer credential: rotate
it if you suspect exposure. Do not rely on source-IP allowlisting as a
substitute control — Pub/Sub and Cloud Run/Functions push traffic does not
originate from a small, stable, documented IP range, so an IP allowlist
either gives a false sense of protection or breaks legitimate delivery when
Google's infrastructure changes. If you need defense in depth beyond the
shared token and the forwarder's OIDC check, restrict who can invoke the
forwarder (for example Cloud Run's own IAM invoker binding, already implied
by `--no-allow-unauthenticated`) rather than filtering by network origin.

OIDC enforcement directly in Eshu is tracked in issue #4659. Enabling it will
land as a Go change plus a paired Helm `oidc` values block in the same PR —
the chart does not expose an inert `oidc` block today because a config
surface that renders no enforcement would imply protection the endpoint does
not have. Once #4659 ships, operators who prefer a direct push subscription
over maintaining a forwarder will be able to drop the forwarder from this
setup.

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

### Push retry semantics

Pub/Sub only counts HTTP `102`,
`200`, `201`, `202`, or `204` from the push endpoint as an acknowledgment
([Push subscriptions](https://cloud.google.com/pubsub/docs/push)). Every
other status is a delivery failure that Pub/Sub retries with backoff, up to
the subscription's message retention window (and to a dead-letter topic if
one is configured). `/webhook/gcp-freshness` returns `401` for a missing or
mismatched shared token and `400` for a payload that does not decode as the
expected `TemporalAsset` shape — both of those are retried, not silently
dropped. Only a successful store and the first-delivery welcome message
return `202`. If the forwarder or Eshu is misconfigured, expect
`numUndeliveredMessages` on the subscription to climb and eventual
dead-lettering (if configured) rather than quiet message loss — that is the
signal to watch, not an absence of errors.

**All deliveries return 401 / `intake_rejected` with an auth reason.** This
almost always means the forwarder is not attaching the shared token, or is
attaching the wrong one. Confirm the Secret referenced by
`webhookListener.gcpFreshness.secretName`/`tokenKey` matches the literal
token value the forwarder sends as `X-Eshu-GCP-Freshness-Token`. Remember
that the request Eshu evaluates comes from the forwarder, not from Pub/Sub
directly — Eshu never sees or checks the forwarder's own inbound OIDC token,
so a "wrong service account" failure at that layer shows up as the forwarder
itself rejecting or failing to invoke (check the forwarder's own logs and
its Cloud Run/Functions invoker IAM binding), not as a 401 from Eshu. If
Eshu is returning 401 despite the forwarder logging a successful send, check
for a proxy, ingress, or load balancer between the forwarder and the
webhook-listener pod that could be stripping the `X-Eshu-GCP-Freshness-Token`
or `Authorization` header.

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
