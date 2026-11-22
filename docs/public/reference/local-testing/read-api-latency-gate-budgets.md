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

Render each table with `--out`, and pass the committed legacy table as the
baseline for both, so the ratchet runs on every row. Without `--out` the default
baseline is the legacy table file, so a stdout render of the winners table would
trip the collapse ratchet on the impact rows (307,638 to 2,403 blocks).

```bash
git show HEAD:testdata/benchmarks/read-api-route-work-budgets.txt > /tmp/baseline-legacy.txt
bash scripts/refresh-read-api-work-budgets.sh --baseline /tmp/baseline-legacy.txt \
  --out testdata/benchmarks/read-api-route-work-budgets.txt \
  --note 'leg=legacy commit=<sha> on origin/main <sha>' LEGACY_REPORT.json...
bash scripts/refresh-read-api-work-budgets.sh --baseline /tmp/baseline-legacy.txt \
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
| Commit | `ca232f7a89` (runs 1 and 2) and `4903719505` (run 3) on `origin/main` `194d4abbd9` | the same |
| Corpus | 800 scopes, 150,000 nodes per label, 150,000 IaC facts, 2,500,000 shared intents, 20,000 finding facts | the same |
| Backend | Neo4j | Neo4j |
| Report sha256 | `4eb538610c25103364876c43aef0a5367a157ec34aa8d711f308cc443e525b3f`, `e9f1fd61e8c01a8f6cdf88b12d3277a944cc6e204329298f943bc9a4d0889d3b`, `210780a45ba0ec8200494d41572ff4aa3026c2166fd448b4614848809d3a74fb` | `36da1fe34fa4c46932a601c4a554fecb7d644b10e778b73e8312d625177179f1`, `e8848c595a042c182d3266af8763ddc7571145bf9b25b0fb3c469bc4b6eff661`, `2a2b0bfeb3c51ac98760889915deb0b7aabad2f48a7f7fa5d1450584f860e1b2` |
| Accept flags | `--accept-drop 'MCP search_entity_content=#8682 ...'` and `--accept-regression 'MCP search_file_content=#10607 ...'`; the full text is in each table header and in the row's reason column | the same two flags (the winners table is rendered against the same committed baseline) |

## The two content-search rows are a new fixture cohort

[#8682](https://github.com/eshu-hq/eshu/issues/8682) (`29753641d7`, 2026-10-27)
renamed the search seed repositories after the work table was last rendered
(2026-10-20). `MCP search_entity_content` falls from 311,960 to 2,652 blocks
because the pilot now finds its page in the first ordered probe. This is a
fixture change, not a faster query, and the old and new numbers are not a
before and after. The row now guards the dense first-probe path only; the
continuation walk is not work-metered by this pilot. Its blocks varied across the
separately seeded runs (396, 400, 642, 884 and 889, with identical calls and rows), so
the budget is three times the maximum of the three reports it was derived from, 884. That is a known limit, not a feature:
the budget is about 6.6 times the low mode (2,652 / 400), so the gate will not
catch a regression of this row smaller than that factor when the row sits in the
low mode. It is 2.2 times wider than the 1,200 blocks a low-mode-derived budget
would have been. The cause of the two modes is not
known; the follow-up issue is [#11224](https://github.com/eshu-hq/eshu/issues/11224).

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
  once per active scope, about three blocks per scope; the product issue is
  [#11208](https://github.com/eshu-hq/eshu/issues/11208).

## Not measured here

The gate starts no reducer and stands in for the infra read model backfill that
the reducer has owned since [#11210](https://github.com/eshu-hq/eshu/pull/11210),
so its infra route numbers describe an install that holds the marker, and a
regression in the reducer's wiring would not show here. Starting the reducer and
dropping the stand-in is [#11226](https://github.com/eshu-hq/eshu/issues/11226).
