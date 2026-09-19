# #6797 Work-Metric Shim: Postgres Buffers Separate The #6794 Fix From Its Parent

Prove-the-theory-first record for the read-API latency gate (#6797). The
theory: a per-request Postgres work counter from `pg_stat_statements`
(buffers, rows, calls) separates the pre-fix and post-fix status routes by
far more than run-to-run latency noise, so a work budget can fail RED and pass
GREEN where an absolute latency ceiling cannot.

## Method

- One seeded stack (800 scopes, 1,120 generations, 67,200 work items, 150,000
  IaC content_entity facts, ANALYZE after seed), kept with `--keep`.
- `pg_stat_statements` loaded with `shared_preload_libraries`,
  `track=all`, `compute_query_id=on`; the extension created afterward.
- For each route: 2 discarded warmup requests, `pg_stat_statements_reset()`,
  20 counted requests, then `sum(calls)`, `sum(rows)` and
  `sum(shared+local+temp blks read/hit/written)` for the current database,
  excluding statements that mention `pg_stat_statements`. Values below are
  per request (sum / 20). One pass per binary; the metric is deterministic.
- RED: eshu-api built from `59c605e48` (before #6808), sha256
  `0540ac5c4d5ed8ce7169f551bd4ff6ce319ef1d9a0d9c98de2c6db2e42521295`.
- GREEN: eshu-api built from `925e8016f` (contains `10055f58e`), sha256
  `bd46d53ecb530ea6819310b33525e5a17e8c5303b34a8ee9a16a0a9ff80f4494`.
- Each run recorded `lsof -p <pid>` resolving to the binary that was hashed.
- Host: developer Docker-on-macOS. These are counter values, not latency, so
  they do not depend on runner CPU speed.

## Result

| route | HTTP RED/GREEN | calls RED | calls GREEN | blks RED | blks GREEN | blks ratio | rows RED | rows GREEN |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `/collectors` | 200/200 | 29.1 | 26.0 | 712894 | 56179 | 12.7 | 20 | 25 |
| `/collector-readiness` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/status/collector-readiness` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/status/collectors` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/status/freshness-causality` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/status/governance` | 200/200 | 30.2 | 27.0 | 737250 | 63020 | 11.7 | 28 | 33 |
| `/status/hosted-readiness` | 200/200 | 29.2 | 26.0 | 737249 | 63019 | 11.7 | 20 | 25 |
| `/status/index` | 200/200 | 25.2 | 22.0 | 735942 | 61712 | 11.9 | 18 | 23 |
| `/status/ingesters` | 200/200 | 25.2 | 22.0 | 735942 | 61712 | 11.9 | 18 | 23 |
| `/status/operations` | 200/200 | 30.1 | 27.0 | 724533 | 67818 | 10.7 | 121 | 126 |
| `/status/operator-control-plane` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/status/pipeline` | 200/200 | 29.2 | 26.0 | 737249 | 63019 | 11.7 | 20 | 25 |
| `/status/semantic-extraction` | 200/200 | 29.1 | 26.0 | 719734 | 63019 | 11.4 | 20 | 25 |
| `/index-status` | 200/200 | 25.1 | 22.0 | 718427 | 61712 | 11.6 | 18 | 23 |
| `/ingesters` | 200/200 | 25.1 | 22.0 | 718427 | 61712 | 11.6 | 18 | 23 |
| `/evidence/bundle` | 200/200 | 29.2 | 26.0 | 737249 | 63019 | 11.7 | 20 | 25 |
| `/repositories` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/repositories/language-inventory` | 200/200 | 2.0 | 2.0 | 1 | 1 | 1.0 | 0 | 0 |
| `/iac/resources` | 200/200 | 2.0 | 2.0 | 108152 | 108152 | 1.0 | 51 | 51 |
| `/freshness/generations` | 200/200 | 2.0 | 2.0 | 19727 | 19727 | 1.0 | 51 | 51 |
| `/infra/resources/count` | 200/200 | 1.0 | 1.0 | 1 | 1 | 1.0 | 0 | 0 |

## Verdict against the ruling's criteria

- **Buffers criterion: proven.** blks RED / blks GREEN is between 10.7 and 12.7
  on all 14 status-family routes that touch Postgres (criterion: at least 10 on
  at least 9 of 14). The thinnest is `/status/operations` at 10.7.
- **Statement count cannot fail RED.** calls move 29.1 -> 26.0 and 25.2 -> 22.0,
  as the ruling predicted from the fix commit's 29 -> 25 round trips.
- **"Every named route RED >= 3x its derived budget" cannot hold literally.**
  Five named routes have identical RED and GREEN counters because #6808 did not
  change them: `/collector-readiness`, `/status/collector-readiness`,
  `/repositories`, `/repositories/language-inventory`, `/iac/resources`.
  A budget derived from GREEN cannot be exceeded 3x by an identical RED. On the
  14 changed routes it holds: RED / budget = ratio / 3 = 3.6 to 4.2 with the
  ruling's `blks = ceil(max_green_blks * 3.0)` formula.
- **Ruling on the criterion (maintainer):** "Read it as 'every route the
  RED/GREEN pair actually changed' (the 14). An identical RED can't exceed a
  GREEN-derived budget by construction. The 5 unchanged routes get GREEN-derived
  work budgets as forward regression guards, documented as not RED-provable
  with this pair." The buffers criterion is therefore proven for the 14 changed
  routes, and the five unchanged routes are guards only.

## Caveat found while running the shim

The gate's bulk infra-label graph seed created one node per label, not 150,000
(`UNWIND range(0, $count - 1) ... CREATE` yields a single node on NornicDB
when the bound is a parameter expression). The Postgres counters above are
unaffected; the graph-backed routes (`/repositories`, `/infra/resources/*`)
were measured against a near-empty infra graph.
