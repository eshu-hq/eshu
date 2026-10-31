# AWS Freshness Trigger Guidance

## Read First

1. `doc.go` - package contract.
2. `types.go` - trigger, target, and durable key rules.
3. `servicekinds.go` - host-owned schedulable service-kind vocabulary.
4. `eventbridge.go` - AWS Config/EventBridge normalization rules.
5. `README.md` - flow, invariants, and telemetry notes.
6. `../../README.md` - coordinator ownership boundary and loop structure.
7. `../../../../workflow/README.md` - workflow claim lifecycle.

## Invariants

- This package resolves to no collector or satellite code
  (`TestProductionFilesHaveNoCollectorImports`). Test-only registry pins are
  the only exception, and they retire with the scanner tree.
- Keep the schedulable vocabulary in `servicekinds.go` pinned to the scanner
  registry (`TestSupportedServiceKindsMatchScannerRegistry`): adding a scanner
  means extending the host list in the same change.
- Treat AWS freshness events as wake-up signals only. They never prove graph,
  workload, deployment, or resource truth.
- Keep trigger targets bounded to one account, one region, and one supported
  service kind.
- Do not add AWS SDK calls here. Event parsing and credentialed AWS reads live
  in runtime adapters.
- Do not put resource ARNs, names, IDs, tags, or raw event payloads in metric
  labels.
- Preserve `aws-global` normalization for IAM, Route 53, and CloudFront so
  global provider events target the same claim boundary as scheduled scans.

## Common Changes

- Add a new event kind by extending `EventKind`, tests in `types_test.go`, and
  telemetry docs that name the bounded label value.
- Change coalescing only when the AWS collector claim shape changes; update the
  Postgres store and coordinator planner in the same PR.

## Do Not

- Do not bypass the workflow claim path.
- Do not make freshness events authoritative over scheduled scans.
- Do not introduce wildcard target support.
