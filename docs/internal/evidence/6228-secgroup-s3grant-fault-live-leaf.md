# #6228 secgroup + s3grant live fault-injection evidence

Families: `security_group_sg_rule`, `security_group_rule_endpoint`,
`s3_external_principal_grant`. Full trio (killworker, statement-hold, failgraphwrite-once)
live GREEN for all three families on 2026-11-02.

## Shard exits

| Shard | Families | Exit |
|---|---|---|
| `6/8` | s3grant trio | 0 |
| `7/8` | sg_rule trio | 0 |
| `8/8` | rule_endpoint trio | 0 |

Logs: `/tmp/6228-secgroup-fault-shard{6,7,8}.log` (gate work dirs retained via `--keep`).

## Non-vacuity markers (observed, not asserted-vacuous)

`s3_external_principal_grant`:
- fault-free baseline retry-attempt count: 0.
- killworker: "1 blocked claimed/running row(s) observed".
- failgraphwrite marker names the targeted GRANTS_ACCESS_TO MERGE.

`security_group_sg_rule`:
- fault-free baseline retry-attempt count: 0.
- killworker: "1 blocked claimed/running row(s) observed".
- failgraphwrite marker names the targeted ALLOWS_* MERGE.

`security_group_rule_endpoint`:
- fault-free baseline retry-attempt count: 0.
- kill-hold engaged: `.statement-held` sentinel present; hold marker
  names the targeted TO MERGE.
- failgraphwrite marker names the targeted TO MERGE.

## Digest recovery

Each fault cell's post-recovery digest matches its family baseline digest
(`assert_matches_baseline`, fail-fast). Example: endpoint family baseline
digest `4312207e…` equals the endpoint failgraphwrite cell digest `4312207e…`.
Shard exit 0 on all three shards means every comparison passed.

## Post-run value scrub

After this run the cassette's synthetic identifiers were scrubbed to the
cassette-author allowlist (same mapping as the determinism leaf note).
Digests above are the pre-scrub run's; static mirrors re-pass on the scrubbed
values and the PR wave re-proves live.
