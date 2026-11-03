# #6228 secgroup + s3grant live fault-injection evidence

Families: `security_group_sg_rule`, `security_group_rule_endpoint`,
`s3_external_principal_grant`. Full trio (killworker, statement-hold, failgraphwrite-once)
live GREEN for all three families on 2026-11-02, re-driven GREEN on the
reviewed head `d2f1f7df31` on 2026-11-03
(`ESHU_DEPLOY_CHECKOUT=/tmp/eshu-deploy`).

## Shard exits

| Shard | Families | Pre-scrub exit | Re-drive exit (`d2f1f7df31`) |
|---|---|---|---|
| `6/8` | s3grant trio | 0 | 0 |
| `7/8` | sg_rule trio | 0 | 0 |
| `8/8` | rule_endpoint trio | 0 | 0 |

Logs: `/tmp/6228-secgroup-fault-shard{6,7,8}.log` (first drive);
`/tmp/fault-redrive-{6,7,8}.log` (re-drive).

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
(`assert_matches_baseline`, fail-fast). Shard exit 0 on all three shards
means every comparison passed.

Re-drive digests on `d2f1f7df31` (baseline = killworker = failgraphwrite
per family):

- `s3_external_principal_grant`: `2710be5117a841dd44f8863ac9d67b2c4ba802990a81d7ae87305a920ec6217c`
  (4/4 exact asserts in all three cells; killworker wall=76s,
  failgraphwrite wall=79s, baseline wall=77s).
- `security_group_sg_rule`: `7b07181c52a511409aafc5b4b8f7bfba9c5070793c32b5d9bc3b53d277d51e10`
  (killworker wall=81s, failgraphwrite wall=141s, baseline wall=141s).
- `security_group_rule_endpoint`: `7b07181c52a511409aafc5b4b8f7bfba9c5070793c32b5d9bc3b53d277d51e10`
  (5/5 exact asserts in all three cells; killworker wall=77s,
  failgraphwrite wall=79s, baseline wall=78s). The endpoint digest equals
  the sg_rule digest because both cells drive the same shared cassette
  through the same graph and the canonicalized dump covers the whole
  graph — corroborating, not colliding with, the determinism re-drive.

## Post-run value scrub

After the first run the cassette's synthetic identifiers were scrubbed to
the cassette-author allowlist (same mapping as the determinism leaf note).
The pre-scrub digests are superseded by the re-drive above; static mirrors
re-pass on the scrubbed values and the PR wave re-proves live.
