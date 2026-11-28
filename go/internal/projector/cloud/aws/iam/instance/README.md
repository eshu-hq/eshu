# AWS IAM instance projector intents

## Purpose

This documentation-only namespace groups instance-related AWS IAM intents.

## Ownership boundary

The `profile` leaf admits profile observations for reducer-owned HAS_ROLE
reconciliation. Root projector keeps lookup, fan-out, and queue ownership.

## Exported surface

None. Import `profile` directly.

## Dependencies

None in this namespace.

## Telemetry

None here. Root projector and reducer retain their existing signals.

## Gotchas / invariants

An empty role list must still trigger the profile leaf so stale edges retract.
