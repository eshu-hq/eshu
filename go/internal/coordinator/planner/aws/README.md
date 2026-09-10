# AWS Planners

## Purpose

`internal/coordinator/planner/aws` groups the AWS freshness and scheduled-scan
planners without merging their responsibilities.

## Ownership boundary

`freshness` plans from claimed AWS freshness triggers. `scheduled` plans from
the periodic `scheduled_scan_enabled` configuration and reuses `freshness`'s
target-scope parser. Each leaf applies the authorization policy for its trigger
mode. The coordinator root retains claim transitions, scheduling, admission,
retries, and telemetry.

## Exported surface

None. Import `aws/freshness` or `aws/scheduled` directly.

See `doc.go` for the package contract.

## Dependencies

None. The namespace package contains documentation only.

## Telemetry

None. AWS scheduling and freshness telemetry remain in the coordinator root.

## Gotchas / invariants

- Keep freshness-trigger and scheduled-scan planning in separate leaves.
- `scheduled` must reuse `freshness.ParseTargetScopes`; keep its global-service
  region policy separate from freshness-trigger authorization.
- Neither child may import the parent coordinator package.

## Related docs

- `go/internal/coordinator/planner/aws/freshness/README.md`
- `go/internal/coordinator/planner/aws/scheduled/README.md`
