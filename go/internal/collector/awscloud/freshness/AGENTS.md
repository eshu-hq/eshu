# AWS Freshness Trigger Guidance

## Read First

1. `doc.go` - package contract.
2. `types.go` - trigger, target, and durable key rules.
3. `README.md` - flow, invariants, and telemetry notes.
4. `../README.md` - AWS collector fact and boundary contract.
5. `../../../workflow/README.md` - workflow claim lifecycle.

## Invariants

- Treat AWS freshness events as wake-up signals only. They never prove graph,
  workload, deployment, or resource truth.
- Keep trigger targets bounded to one account, one region, and one supported
  service kind.
- Do not add AWS SDK calls here. Event parsing and credentialed AWS reads live
  in runtime adapters.
- Do not put resource ARNs, names, IDs, tags, or raw event payloads in metric
  labels.

## Common Changes

- Add a new event kind by extending `EventKind`, tests in `types_test.go`, and
  telemetry docs that name the bounded label value.
- Change coalescing only when the AWS collector claim shape changes; update the
  Postgres store and coordinator planner in the same PR.

## Do Not

- Do not bypass the workflow claim path.
- Do not make freshness events authoritative over scheduled scans.
- Do not introduce wildcard target support.
