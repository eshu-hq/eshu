# Read API Latency Gate: Work Budget Tables

The [read API latency gate](read-api-latency-gate.md) sweeps two legs against
one seed and enforces a Postgres work budget table on each:

| Leg | Core flag | Table |
| --- | --- | --- |
| Legacy | `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=false` (the default) | `testdata/benchmarks/read-api-route-work-budgets.txt` |
| Winners | `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=true` | `testdata/benchmarks/read-api-route-work-budgets-winners.txt` |

The legacy table guards the default configuration. The winners table guards the
maintained read model: the two impact aggregate routes read about 128 times
fewer blocks from it, so a table derived from the legacy read cannot guard it.
`GATE_WORK_BUDGETS` and `GATE_WINNERS_WORK_BUDGETS` override the paths.

Both tables are rendered by `scripts/refresh-read-api-work-budgets.sh`, never by
hand, from at least two work reports per leg taken from two separately seeded
full-corpus runs. A table that was derived from a run is not confirmed by that
run: a third run with the committed tables must exit 0 on both legs.

## Re-rendering a table

Render each table with `--out`, against its own committed copy as the baseline,
so the ratchet runs on every row. The winners rows are far smaller than the legacy
rows by design (the two impact routes read 801 blocks from the winners table and
102,546 from the legacy read), so baselining the winners render on the legacy
table trips the collapse ratchet on those rows (307,638 to 2,403 blocks) and
exits 3. Do not pass `--accept-drop` to get past that: it would accept a
comparison across two different read paths. Without `--baseline`, an `--out`
render uses the file it is replacing.

```bash
git show HEAD:testdata/benchmarks/read-api-route-work-budgets.txt > /tmp/baseline-legacy.txt
git show HEAD:testdata/benchmarks/read-api-route-work-budgets-winners.txt > /tmp/baseline-winners.txt
bash scripts/refresh-read-api-work-budgets.sh --baseline /tmp/baseline-legacy.txt \
  --out testdata/benchmarks/read-api-route-work-budgets.txt \
  --note 'leg=legacy commit=<sha> on origin/main <sha>' LEGACY_REPORT.json...
bash scripts/refresh-read-api-work-budgets.sh --baseline /tmp/baseline-winners.txt \
  --out testdata/benchmarks/read-api-route-work-budgets-winners.txt \
  --note 'leg=winners commit=<sha> on origin/main <sha>' WINNERS_REPORT.json...
```

Add an `--accept-drop` or `--accept-regression` flag only for a row the ratchet
refuses; the table header and the row's reason column then name it, and an unused
flag prints nothing. The header's first line names the file it was rendered for.

## Provenance

Each table's header restates what it was derived from: the sha256 of every input
report, each accept flag used, and notes naming the leg, the commit, and the
corpus. A row accepted past a ratchet also carries its issue in its reason
column.

| | Legacy table | Winners table |
| --- | --- | --- |
| Commit | `25c35c5c7d` (three runs, the final seed code) on `origin/main` `74bb28e1e7` | the same |
| Corpus | 800 scopes, 150,000 nodes per label, 150,000 IaC facts, 2,500,000 shared intents, 20,000 finding facts | the same |
| Backend | Neo4j | Neo4j |
| Report sha256 | `7a8b4dddfe19a3beb0c5be5c9237b86eb09cea706a82ac12d116922757e9f615`, `73fbe265b839baeb2cd4717c93c2f3599033dafc4bec60e0d50d2c11fb4557b4`, `4bf73ee0df44709943ee47c10aceab4d2514614ed726e410330abbf7cb409e29` | `d2b91b6811c0d341f3317dccdfee066c655ba7c961e7951979f014c5cb92e12a`, `05bb84e318b473b2903b28d921fb28750eed37b85fdba1f32bf54013038daaa3`, `195f711c94f1baac4edf029cd481bae7622429bbc9759443f9de5668be7398ed` |
| Accept flags | none: the ratchet needed none (against the committed table the entity row falls 5.5x, under the 20x drop factor, and no other row moved past a threshold) | none |

The earlier acceptances (the entity row under #8682, the file-search row under
#10607) were flags of the previous render; this render records them in header notes.

## The two content-search rows are a new fixture cohort

[#8682](https://github.com/eshu-hq/eshu/issues/8682) (`29753641d7`, 2026-10-27)
renamed the search seed repositories after the work table was last rendered
(2026-10-20). `MCP search_entity_content` fell from 311,960 to 2,652 blocks
because the pilot now finds its page in the first ordered probe. This is a
fixture change, not a faster query, and the old and new numbers are not a
before and after. The row guards the dense first-probe path only; the
continuation walk is not work-metered by this pilot.

The 2,652 budget was derived from a row that read 396, 400, 642, 884 and 889
blocks across five separately seeded runs, with identical calls and rows. The
cause was the visibility map: the walk's second arm is an Index Only Scan, and
how many heap pages it visited depended on how far autovacuum had got when the
sweep began ([#11224](https://github.com/eshu-hq/eshu/issues/11224); see
[Seed Notes](read-api-latency-gate-seed.md)). The seed now ends with
`VACUUM (ANALYZE)`, and the row reads 157, 158 and 162 blocks in three
separately seeded runs, so its budget is 486 (3 x 162). The 6.6x slack the wide
budget carried is gone.

`MCP search_file_content` rises from 798 to 1,740 blocks. The prefix change does
not affect that route's ordering. Per statement, one request costs 14 calls, 29
rows and 580 blocks: the keyed snippet re-read that
[#10607](https://github.com/eshu-hq/eshu/issues/10607) added (1 call, 10 rows,
297 blocks), the walk (1 call, 12 rows, 128 blocks), the content-index readiness
probes and their nested statements (62, 60, 2, 15 and 15 blocks), the auth token
lookup (1 block), and two read-only transactions (`begin`, `set_config`,
`rollback`). The re-read is 297 of the 314-block growth. The remaining 17 blocks
(the nested lower-trigram `NOT EXISTS` and the state read) are not shown to
come from the 2026-10-20 window.

## What the default row is

The `default` row comes from the largest unnamed route. On this corpus that is
`GET /api/v0/documentation/facts` at 36 blocks per request, so the default is 108
blocks (it was 33). All 36 blocks are the route's own statements: 32 for its
facts SELECT (a keyset index scan that removes about 25 facts per scope by
`fact_kind`), 3 for the scope status lookup, and 1 for the auth token lookup. The
growth is the 20,000 seeded finding facts, about 25 per scope. No core background
statement landed in the measurement window. At 108 the default blocks counter is
above the ratchet floor (100), so the ratchet covers the default row from the next
render.

## Other rows that moved more than 25%

The committed rows were rendered 2026-10-20, before the gate stopped being
runnable. The rows below moved against them. For each, the new value is shown;
the old value is not reproducible and is not attributed, because the old
topology, including the removed in-tree API's startup backfill, is gone.

- `infra/resources/count`, `infra/resources/inventory` and `iac/resources`
  (149,436 to 74,742 and 149,622 to 74,928 blocks, both legs): one statement reads
  the whole `infra_resource_entities` table, 24,910 blocks per request. The table
  is 24,894 heap blocks and 1,050,000 rows with no dead tuples, and
  3 x 24,914 = 74,742.
- `repositories/{repo_id}/story` calls (389 to 197): the route makes 33 + 38 + 60
  keyed `content_files` calls plus 3 others, for the 33 seeded story repositories.
- `freshness/generations` (92,858 to 59,696 blocks, both legs): it reads 19,899
  blocks in each of the three runs with the seed vacuum, against 30,953 in each of
  the five runs before it. That is consistent with fewer heap fetches once the
  visibility map is set; the plan was not checked, so it is a theory.
- Every named row at the default row's floor (capabilities, the two
  collector-readiness rows, graph/entities, repositories, semantic-extraction,
  container-images identities count) and the floored counters of the others: the
  cause is the default row, by the script's floor rule.
- `supply-chain/impact/findings/count` (33 to 307,638 legacy, 2,403 winners): the
  old row was the default floor on an empty impact corpus; the growth is the 20,000
  seeded finding facts. The script does not ratchet a committed counter under 100,
  so no accept flag is used.

## Named rows added for this gate

- `GET /api/v0/supply-chain/impact/inventory` joins `findings/count` as a named
  row on both legs. Left unnamed it would have set the default row to about
  307,638 blocks and disarmed every unnamed route.
- `GET /api/v0/cloud/ssm-host-inventory` is named with its measured 2,455 blocks
  (legacy and winners alike). With no SSM facts it still probes the fact index
  once per active scope. The measured cost of about 3.0 buffers per active scope
  (2,455 at 800 scopes) is accepted by the arbiter ruling in
  #11208 (retired evidence record);
  [#11208](https://github.com/eshu-hq/eshu/issues/11208) is being closed as not
  planned.

## Not measured here

The gate stands in, permanently, for the infra read model backfill that the
reducer has owned since [#11210](https://github.com/eshu-hq/eshu/pull/11210): a
real reducer cannot run on the seeded corpus (see
[Seed Notes](read-api-latency-gate-seed.md)), so its infra route numbers describe
an install that holds the marker. The reducer-level proof is a live test in
`go/cmd/reducer` ([#11226](https://github.com/eshu-hq/eshu/issues/11226)).
