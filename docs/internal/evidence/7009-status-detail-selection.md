# Repository ingester status selection (#7009)

## Scope and truth

Repository ingester detail reads queue, coordinator, scope activity, stage
summaries, domain backlogs, and health. Terraform-state serials and warnings
are absent from that response. Its selection omits those two evidence
statements. Full pipeline and index status still read them and propagate
their failures. The MCP `get_ingester_status` tool dispatches to the same
detail handler. The read-only repeatable-read transaction, checkpoint,
timeout, and health evaluator are unchanged.

A failing-first handler regression returned HTTP 500 when only the unused
Terraform evidence query failed. With the detail selection, both canonical
and legacy repository detail routes return HTTP 200 and keep their health,
queue, coordinator, scope activity, stage, and backlog fields. Full and index
routes still return HTTP 500 for that failure. Another required status read
failure still returns HTTP 500 on repository detail. An empty-store statement
inventory confirms that only the two Terraform statements are omitted.

## Performance Evidence:

On the ops-qa reader snapshot, the Terraform serial query took 0.671 ms for
four rows, and the recent-warning query took 360.839 ms for 92 rows. Those
statements are absent from the repository detail response's input selection,
so this change removes them from that route. These are individual SQL timings;
built HTTP/MCP endpoint p95, physical cold latency, deployed p95, and
100-user capacity remain NOT_CHECKED. Full pipeline and index routes continue
to pay the cost of these required reads.

## No-Observability-Change:

The existing `eshu_dp_status_snapshot_read_duration_seconds` histogram and
bounded read labels remain. A detail read that omits Terraform evidence emits
no `terraform_state` phase sample; full and index status retain that sample
and propagate its errors. The `status_snapshot` span and database query
summary labels remain available for operator attribution.

## Local binary route check

On a disposable primary with a separate read-only session pool against that
same primary, the baseline and candidate API/MCP binaries each completed 42
repository-detail calls over 800 scopes, 21,600 generations, and 192,001 work
rows. All 84 decoded payloads matched after excluding version and elapsed-age
fields that change with request time. This checks the built route and MCP
selector on that local profile; it does not establish physical standby,
deployed, cold-read, concurrent-user, or 100-user performance. Local warm p95
was recorded for both binaries, but quiet-host comparability was not checked,
so no endpoint speedup is claimed.
