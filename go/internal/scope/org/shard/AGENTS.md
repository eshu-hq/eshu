# AGENTS.md — go/internal/scope/org/shard

Scope: `go/internal/scope/org/shard` only. The parent instructions in
`go/internal/scope/AGENTS.md` still apply.

## Read first

1. `go/internal/scope/org/shard/README.md`: the partition rules.
2. `go/internal/scope/org/shard/doc.go`: the package contract.
3. `go/internal/coordinator/security/alert/planner.go`: the planner that turns
   shards into work items and retires unplanned shard scopes.

## Invariants

- This is the only partition and the only owner/name normalization for
  security-alert targets. Do not mirror `Partition`, `ScopeID`, or
  `NormalizeRepository` into the planner, the collector, or the query layer;
  three copies drifting is a wrong-coverage bug.
- The hash (first 8 bytes of sha256, big endian, MSB first), the split rule
  (more than 100 splits), the depth cap (32), and the name format
  (`<base>#shard-<full bits>`, base unchanged for one shard) are durable
  identity. `fixed_test.go` pins the measured rows; a change that moves them
  renames stored scopes and needs a migration plus the retirement of the old
  names.
- `Partition` stays a pure function of its inputs: no stored state, no
  hysteresis, no dependence on allowlist order.
- Keep the dependency set to the standard library. The coordinator, collector,
  and query packages import this one.
- A shard id never carries a repository name.

## Common changes

- A new reader of org targets calls `Partition` and `NormalizeRepository`
  here, and validation must keep rejecting operator scope ids for which
  `HasMarker` is true.
