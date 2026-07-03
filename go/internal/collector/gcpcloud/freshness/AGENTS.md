# GCP Freshness Trigger Guidance

## Read First

1. `doc.go` - package contract.
2. `types.go` - trigger, target, and durable key rules.
3. `pubsub.go` - Cloud Asset Inventory (CAI) Pub/Sub push normalization rules.
4. `README.md` - flow, invariants, and telemetry notes.
5. `../README.md` - GCP collector fact and boundary contract.
6. `../../../workflow/README.md` - workflow claim lifecycle.
7. `../../awscloud/freshness/README.md` - the AWS mirror this package follows.

## Invariants

- Treat CAI feed notifications as wake-up signals only. They never prove
  graph, workload, deployment, or resource truth.
- Keep trigger targets bounded to one parent scope kind, one parent scope id,
  one asset type, and one location.
- Do not add GCP SDK or Pub/Sub client calls here. Push delivery, ack
  handling, and credentialed GCP reads live in runtime adapters
  (`go/cmd/webhook-listener`) and future collector runtime code.
- Do not put resource names, IDs, labels, IAM members, or raw CAI resource
  data / push payload bodies in metric labels.
- The CAI `ancestors` chain lists the most specific parent first; preserve
  that "index 0 wins" derivation so the trigger target matches the collector
  claim shard shape.
- Preserve the welcome-message detection in `NormalizePubSubPush`. The first
  delivery to a new feed subscription is a bare JSON string, not a
  `TemporalAsset`; it must return `ErrWelcomeMessage`, never a malformed-event
  error.

## Common Changes

- Add a new event kind by extending `EventKind`, tests in `types_test.go` and
  `pubsub_test.go`, and telemetry docs that name the bounded label value.
- Change coalescing only when the GCP collector claim shape changes; update
  the Postgres store and coordinator planner in the same PR.

## Do Not

- Do not bypass the workflow claim path.
- Do not make freshness events authoritative over scheduled scans.
- Do not introduce wildcard target support.
- Do not decode or retain the CAI `resource.data` blob, IAM policy bindings,
  or any other asset content beyond `assetType`, `ancestors`, and
  `resource.location`.
