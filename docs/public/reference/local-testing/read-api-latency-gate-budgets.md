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

## Provenance

Each table's header restates what it was derived from: the sha256 of every input
report, each accept flag used, and notes naming the leg, the commit, and the
corpus. A row accepted past a ratchet also carries its issue in its reason
column.

| | Legacy table | Winners table |
| --- | --- | --- |
| Commit | `ca232f7a89` on `origin/main` `194d4abbd9` | the same |
| Corpus | 800 scopes, 150,000 nodes per label, 150,000 IaC facts, 2,500,000 shared intents, 20,000 finding facts | the same |
| Backend | Neo4j | Neo4j |
| Report sha256 | `4eb538610c25103364876c43aef0a5367a157ec34aa8d711f308cc443e525b3f`, `e9f1fd61e8c01a8f6cdf88b12d3277a944cc6e204329298f943bc9a4d0889d3b` | `36da1fe34fa4c46932a601c4a554fecb7d644b10e778b73e8312d625177179f1`, `e8848c595a042c182d3266af8763ddc7571145bf9b25b0fb3c469bc4b6eff661` |
| Accept flags | recorded when the legacy table is committed; two rows the ratchet refused await a ruling | none (new table, no baseline) |

## What the default row is

The `default` row comes from the largest unnamed route. On this corpus that is
`GET /api/v0/documentation/facts` at 36 blocks per request, so the default is 108
blocks (it was 33). All 36 blocks are the route's own statements: 32 for its
facts SELECT (a keyset index scan that removes about 25 facts per scope by
`fact_kind`), 3 for the scope status lookup, and 1 for the auth token lookup. The
growth is the 20,000 seeded finding facts, about 25 per scope. No core background
statement landed in the measurement window.

## Named rows added for this gate

- `GET /api/v0/supply-chain/impact/inventory` joins `findings/count` as a named
  row on both legs. Left unnamed it would have set the default row to about
  307,638 blocks and disarmed every unnamed route.
- `GET /api/v0/cloud/ssm-host-inventory` is named with its measured 2,455 blocks
  (legacy and winners alike). With no SSM facts it still probes the fact index
  once per active scope, about three blocks per scope; the product issue is
  [#11208](https://github.com/eshu-hq/eshu/issues/11208).

## Not measured here

The gate stands in for the infra read model backfill that no product runtime
runs today ([#11196](https://github.com/eshu-hq/eshu/issues/11196)), so its infra
route numbers describe an install that holds the marker. A fresh install serves
those routes from the graph, and this gate does not measure that path.
