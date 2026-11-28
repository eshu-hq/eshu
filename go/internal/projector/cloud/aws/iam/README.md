# AWS IAM projector intents

## Purpose

This documentation-only namespace groups AWS IAM reducer-intent builders.

## Ownership boundary

`trust` selects CAN_ASSUME trust statements. `instance/profile` selects
instance-profile observations for HAS_ROLE reconciliation. Neither writes
graph edges or owns the projector queue.

## Exported surface

None. Import the owning leaf directly.

## Dependencies

None in this namespace.

## Telemetry

None here. Root projector and reducer retain their existing signals.

## Gotchas / invariants

Both leaves preserve the shared `aws_resource_materialization:<scope>`
readiness key; do not make it family-specific during a path move.
