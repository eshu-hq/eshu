# Security-Alert Organization Shards

## Purpose

`internal/scope/org/shard` decides how a security-alert organization target
splits into bounded shard scopes (#9019). One org claim reads at most 100
allowlisted repositories one by one after a truncated org read, so a target
with more same-org repositories used to leave the rest unread on every claim.
The target is now planned as shards of at most 100 repositories each.

Three components read the same target configuration on their own: the
coordinator's security-alert planner, the security-alert collector, and the
query layer's collection-state classifier. They all call `Partition` here, so
they agree on the shard ids and on which shard holds which repository.

## Ownership boundary

This package owns the partition and the shard id format. It does not plan
work (`internal/coordinator/security/alert`), read providers or commit
generations (`internal/collector/securityalerts/alertruntime`), retire scopes
(the planner plans retirement, the collector answers it), or classify read
answers (`internal/query/supply/chain`).

| Rule | Definition |
|---|---|
| Input | The distinct same-org repositories of the allowlist, normalized with `NormalizeRepository`. Duplicates, case variants, malformed entries, and other owners' repositories are dropped and never count toward the cap. |
| Hash | First 8 bytes of sha256 over the normalized `owner/name`, big endian, read most significant bit first. Fixed forever. |
| Split | A node holding more than `MaxRepositories` (100) splits on its next bit. Empty leaves are dropped. |
| Depth cap | `MaxDepth` (32). A leaf at the cap still over 100 is returned with `OverCap`; the collector's per-claim fan-out cap and its `security_alert.org_fanout_over_cap` warning are the backstop. |
| Names | One shard: the operator's base `scope_id`, unchanged. More than one: every leaf is `<base>#shard-<bits>` with the full bit prefix (no trailing-zero strip). The suffix is at most 39 bytes and never carries a repository name. |
| Order | Shards are ordered by bit prefix; repositories inside a shard keep allowlist order, so the #8946 fan-out cursor walks them as before. |

There is no hysteresis: the shard set is a pure function of the
configuration. Adding one repository retires at most one shard (the leaf that
splits) and adds at most two; removing one can merge two leaves back into
their parent. The coordinator retires every shard scope that is no longer
planned.

Measured shard counts and fill (seed 9019 random allowlists, pinned by
`fixed_test.go`): N=101 gives K=2 (50 to 51 per shard), N=200 K=3 (52 to 94),
N=500 K=8 (51 to 75), N=999 K=16 (48 to 75), N=4985 K=64 (63 to 96).

## Exported surface

- `Partition`, `Leaf`, `For` — the partition and its lookup.
- `ScopeID`, `ParseScopeID`, `HasMarker`, `Marker`, `MaxSuffixLength` —
  the shard id format.
- `NormalizeRepository`, `NormalizeOrganization` — the shared normalization.
- `MaxRepositories`, `MaxDepth` — the split bounds.

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`crypto/sha256`, `encoding/binary`, `slices`,
`strings`).

## Telemetry

None. The package is pure computation; the planner and the collector record
shard counts and sizes on their own logs and spans.

## Gotchas / invariants

- The hash, split rule, depth cap, and name format are durable identity;
  `fixed_test.go` pins them.
- A shard id never carries a repository name.
- Configuration validation rejects an operator `scope_id` containing
  `#shard-`; the query decoder drops one.

## Related docs

- `docs/public/reference/security-intelligence-provider-alert-parity.md` (org targets and sharding)
